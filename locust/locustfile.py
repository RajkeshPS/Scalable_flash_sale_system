from locust import HttpUser, task, between, events
import time

ALB = "http://flash-sale-alb-2043999948.us-east-1.elb.amazonaws.com"

class PurchaseUser(HttpUser):
    wait_time = between(0.1, 0.5)
    host = ALB

    @task
    def purchase(self):
        self.client.post("/purchase")

class AggressivePurchaseUser(HttpUser):
    """Used for Exp 3 — 5000 users with no wait time"""
    wait_time = between(0, 0.1)
    host = ALB

    @task
    def purchase(self):
        self.client.post("/purchase")
