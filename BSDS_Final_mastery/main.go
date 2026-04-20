package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

type Config struct {
	Port        string
	DBHost      string
	DBPort      string
	DBUser      string
	DBPass      string
	DBName      string
	S3Bucket    string
	S3Region    string
	WorkerCount int
}

func loadConfig() Config {
	return Config{
		Port:        envOrDefault("PORT", "8080"),
		DBHost:      envOrDefault("DB_HOST", "localhost"),
		DBPort:      envOrDefault("DB_PORT", "5432"),
		DBUser:      envOrDefault("DB_USER", "postgres"),
		DBPass:      envOrDefault("DB_PASS", "postgres"),
		DBName:      envOrDefault("DB_NAME", "albumstore"),
		S3Bucket:    envOrDefault("S3_BUCKET", "album-store-photos"),
		S3Region:    envOrDefault("S3_REGION", "us-west-2"),
		WorkerCount: envOrDefaultInt("WORKER_COUNT", 30),
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envOrDefaultInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

type PhotoJob struct {
	PhotoID string
	AlbumID string
	S3Key   string
	Data    []byte
}

type App struct {
	cfg        Config
	db         *sql.DB
	s3client   *s3.S3
	s3uploader *s3manager.Uploader
	jobChan    chan PhotoJob
	wg         sync.WaitGroup
}

func main() {
	cfg := loadConfig()

	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=require",
		cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPass, cfg.DBName,
	)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("db open: %v", err)
	}
	db.SetMaxOpenConns(60)
	db.SetMaxIdleConns(30)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(2 * time.Minute)

	if err := db.Ping(); err != nil {
		log.Fatalf("db ping: %v", err)
	}
	log.Println("Connected to PostgreSQL")

	if err := runMigrations(db); err != nil {
		log.Fatalf("migrations: %v", err)
	}

	sess, err := session.NewSession(&aws.Config{
		Region:          aws.String(cfg.S3Region),
		S3UseAccelerate: aws.Bool(true),
		MaxRetries:      aws.Int(2),
	})
	if err != nil {
		log.Fatalf("aws session: %v", err)
	}
	s3client := s3.New(sess)

	s3uploader := s3manager.NewUploaderWithClient(s3client, func(u *s3manager.Uploader) {
		u.PartSize = 5 * 1024 * 1024
		u.Concurrency = 10
	})

	app := &App{
		cfg:        cfg,
		db:         db,
		s3client:   s3client,
		s3uploader: s3uploader,
		jobChan:    make(chan PhotoJob, 5000),
	}

	for i := 0; i < cfg.WorkerCount; i++ {
		app.wg.Add(1)
		go app.photoWorker(i)
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.MaxMultipartMemory = 512 << 20

	r.GET("/health", app.healthHandler)
	r.PUT("/albums/:album_id", app.putAlbumHandler)
	r.GET("/albums/:album_id", app.getAlbumHandler)
	r.GET("/albums", app.listAlbumsHandler)
	r.POST("/albums/:album_id/photos", app.uploadPhotoHandler)
	r.GET("/albums/:album_id/photos/:photo_id", app.getPhotoHandler)
	r.DELETE("/albums/:album_id/photos/:photo_id", app.deletePhotoHandler)

	srv := &http.Server{
		Addr:           ":" + cfg.Port,
		Handler:        r,
		ReadTimeout:    120 * time.Second,
		WriteTimeout:   120 * time.Second,
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	go func() {
		log.Printf("Listening on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
	close(app.jobChan)
	app.wg.Wait()
	db.Close()
	log.Println("Bye")
}

func runMigrations(db *sql.DB) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS albums (
			album_id    TEXT PRIMARY KEY,
			title       TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			owner       TEXT NOT NULL DEFAULT '',
			photo_count INT  NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS photos (
			photo_id TEXT PRIMARY KEY,
			album_id TEXT NOT NULL REFERENCES albums(album_id),
			seq      INT  NOT NULL,
			status   TEXT NOT NULL DEFAULT 'processing',
			url      TEXT NOT NULL DEFAULT '',
			s3_key   TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_photos_album_id ON photos(album_id)`,
		`CREATE INDEX IF NOT EXISTS idx_photos_status ON photos(status)`,
	}
	for _, q := range queries {
		if _, err := db.Exec(q); err != nil {
			return fmt.Errorf("migration failed: %w — query: %s", err, q)
		}
	}
	log.Println("Migrations complete")
	return nil
}

func (a *App) healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (a *App) putAlbumHandler(c *gin.Context) {
	albumID := c.Param("album_id")

	var req struct {
		AlbumID     string `json:"album_id"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Owner       string `json:"owner"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	if req.AlbumID == "" {
		req.AlbumID = albumID
	}

	var existed bool
	err := a.db.QueryRow(`
		INSERT INTO albums (album_id, title, description, owner)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (album_id) DO UPDATE
			SET title = EXCLUDED.title,
			    description = EXCLUDED.description,
			    owner = EXCLUDED.owner
		RETURNING (xmax::text::int > 0)
	`, albumID, req.Title, req.Description, req.Owner).Scan(&existed)
	if err != nil {
		log.Printf("putAlbum error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}

	status := http.StatusCreated
	if existed {
		status = http.StatusOK
	}

	c.JSON(status, gin.H{
		"album_id":    albumID,
		"title":       req.Title,
		"description": req.Description,
		"owner":       req.Owner,
	})
}

func (a *App) getAlbumHandler(c *gin.Context) {
	albumID := c.Param("album_id")

	var title, desc, owner string
	err := a.db.QueryRow(
		`SELECT title, description, owner FROM albums WHERE album_id = $1`,
		albumID,
	).Scan(&title, &desc, &owner)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if err != nil {
		log.Printf("getAlbum error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"album_id":    albumID,
		"title":       title,
		"description": desc,
		"owner":       owner,
	})
}

func (a *App) listAlbumsHandler(c *gin.Context) {
	rows, err := a.db.Query(`SELECT album_id, title, description, owner FROM albums`)
	if err != nil {
		log.Printf("listAlbums error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	defer rows.Close()

	albums := make([]gin.H, 0)
	for rows.Next() {
		var aid, title, desc, owner string
		if err := rows.Scan(&aid, &title, &desc, &owner); err != nil {
			log.Printf("listAlbums scan: %v", err)
			continue
		}
		albums = append(albums, gin.H{
			"album_id":    aid,
			"title":       title,
			"description": desc,
			"owner":       owner,
		})
	}

	c.JSON(http.StatusOK, albums)
}

func (a *App) uploadPhotoHandler(c *gin.Context) {
	albumID := c.Param("album_id")

	file, _, err := c.Request.FormFile("photo")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing photo field"})
		return
	}
	defer file.Close()

	photoID := uuid.New().String()
	s3Key := fmt.Sprintf("photos/%s/%s", albumID, photoID)

	// Start S3 upload concurrently while we read the file
	pr, pw := io.Pipe()
	var uploadErr error
	var uploadDone sync.WaitGroup
	uploadDone.Add(1)
	go func() {
		defer uploadDone.Done()
		_, uploadErr = a.s3uploader.Upload(&s3manager.UploadInput{
			Bucket:      aws.String(a.cfg.S3Bucket),
			Key:         aws.String(s3Key),
			Body:        pr,
			ContentType: aws.String("image/jpeg"),
		})
		if uploadErr != nil {
			log.Printf("streaming s3 upload error: %v", uploadErr)
		}
	}()

	// Read file and write to pipe simultaneously
	_, err = io.Copy(pw, file)
	pw.Close()
	if err != nil {
		pr.Close()
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read photo"})
		return
	}

	// Do DB work while S3 upload continues in background
	var seq int
	err = a.db.QueryRow(`
		UPDATE albums SET photo_count = photo_count + 1
		WHERE album_id = $1
		RETURNING photo_count
	`, albumID).Scan(&seq)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "album not found"})
		return
	}
	if err != nil {
		log.Printf("seq assignment error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "seq error"})
		return
	}

	_, err = a.db.Exec(`
		INSERT INTO photos (photo_id, album_id, seq, status, s3_key)
		VALUES ($1, $2, $3, 'processing', $4)
	`, photoID, albumID, seq, s3Key)
	if err != nil {
		log.Printf("photo insert error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}

	// Queue the completion update — worker just waits for S3 and updates DB
	a.jobChan <- PhotoJob{
		PhotoID: photoID,
		AlbumID: albumID,
		S3Key:   s3Key,
	}

	c.JSON(http.StatusAccepted, gin.H{
		"photo_id": photoID,
		"seq":      seq,
		"status":   "processing",
	})
}

func (a *App) getPhotoHandler(c *gin.Context) {
	albumID := c.Param("album_id")
	photoID := c.Param("photo_id")

	var pid, aid, status, url string
	var seq int
	err := a.db.QueryRow(`
		SELECT photo_id, album_id, seq, status, url
		FROM photos WHERE photo_id = $1 AND album_id = $2
	`, photoID, albumID).Scan(&pid, &aid, &seq, &status, &url)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if err != nil {
		log.Printf("getPhoto error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}

	resp := gin.H{
		"photo_id": pid,
		"album_id": aid,
		"seq":      seq,
		"status":   status,
	}
	if status == "completed" && url != "" {
		resp["url"] = url
	}

	c.JSON(http.StatusOK, resp)
}

func (a *App) deletePhotoHandler(c *gin.Context) {
	albumID := c.Param("album_id")
	photoID := c.Param("photo_id")

	var s3Key string
	err := a.db.QueryRow(
		`SELECT s3_key FROM photos WHERE photo_id = $1 AND album_id = $2`,
		photoID, albumID,
	).Scan(&s3Key)

	if err == sql.ErrNoRows {
		c.Status(http.StatusNoContent)
		return
	}
	if err != nil {
		log.Printf("deletePhoto lookup error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}

	_, err = a.db.Exec(
		`DELETE FROM photos WHERE photo_id = $1 AND album_id = $2`,
		photoID, albumID,
	)
	if err != nil {
		log.Printf("deletePhoto db error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}

	if s3Key != "" {
		a.s3client.DeleteObject(&s3.DeleteObjectInput{
			Bucket: aws.String(a.cfg.S3Bucket),
			Key:    aws.String(s3Key),
		})
	}

	c.Status(http.StatusNoContent)
}

func (a *App) photoWorker(id int) {
	defer a.wg.Done()
	log.Printf("Worker %d started", id)
	for job := range a.jobChan {
		a.completePhoto(job)
	}
	log.Printf("Worker %d stopped", id)
}

func (a *App) completePhoto(job PhotoJob) {
	// If Data is provided (fallback), upload it
	if job.Data != nil {
		_, err := a.s3uploader.Upload(&s3manager.UploadInput{
			Bucket:      aws.String(a.cfg.S3Bucket),
			Key:         aws.String(job.S3Key),
			Body:        bytes.NewReader(job.Data),
			ContentType: aws.String("image/jpeg"),
		})
		if err != nil {
			log.Printf("worker s3 upload error for %s: %v", job.PhotoID, err)
			a.db.Exec(`UPDATE photos SET status='failed' WHERE photo_id=$1`, job.PhotoID)
			return
		}
	}

	// Wait briefly for streaming upload to finish if it was started in handler
	// Then verify the object exists
	for retries := 0; retries < 120; retries++ {
		_, err := a.s3client.HeadObject(&s3.HeadObjectInput{
			Bucket: aws.String(a.cfg.S3Bucket),
			Key:    aws.String(job.S3Key),
		})
		if err == nil {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}

	photoURL := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s",
		a.cfg.S3Bucket, a.cfg.S3Region, job.S3Key)

	_, err := a.db.Exec(`
		UPDATE photos SET status='completed', url=$1, s3_key=$2
		WHERE photo_id=$3
	`, photoURL, job.S3Key, job.PhotoID)
	if err != nil {
		log.Printf("worker db update error for %s: %v", job.PhotoID, err)
	}
}
