package queue

import (
	"context"
	"log"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"flash-sale/internal/stock"
)

var sqsClient *sqs.Client

// InitSQS initializes the SQS client.
// Called once at startup from main.go when SQS_QUEUE_URL is set.
func InitSQS(ctx context.Context) error {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return err
	}
	sqsClient = sqs.NewFromConfig(cfg)
	return nil
}

// SendPurchase enqueues a purchase message to SQS.
func SendPurchase(ctx context.Context, queueURL string) error {
	_, err := sqsClient.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(queueURL),
		MessageBody: aws.String("purchase"),
	})
	return err
}

// StartWorker polls SQS and processes purchase messages.
// Runs as a goroutine in the background.
func StartWorker(ctx context.Context, queueURL string, backend stock.Backend) {
	log.Println("SQS worker started, polling:", queueURL)

	for {
		select {
		case <-ctx.Done():
			log.Println("SQS worker shutting down")
			return
		default:
			msgs, err := sqsClient.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
				QueueUrl:            aws.String(queueURL),
				MaxNumberOfMessages: 10,
				WaitTimeSeconds:     5, // long polling
			})
			if err != nil {
				log.Printf("SQS receive error: %v", err)
				continue
			}

			for _, msg := range msgs.Messages {
				// Process the purchase
				err := backend.Purchase(ctx)
				if err != nil {
					if err == stock.ErrSoldOut {
						log.Println("SQS worker: sold out, discarding message")
					} else {
						log.Printf("SQS worker purchase error: %v", err)
						continue // don't delete — retry
					}
				}

				// Delete message from queue after processing
				_, err = sqsClient.DeleteMessage(ctx, &sqs.DeleteMessageInput{
					QueueUrl:      aws.String(queueURL),
					ReceiptHandle: msg.ReceiptHandle,
				})
				if err != nil {
					log.Printf("SQS delete error: %v", err)
				}
			}
		}
	}
}