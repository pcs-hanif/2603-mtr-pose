# MTR Pose Pub/Sub Receiver

Aplikasi untuk menerima pesan dari Google Pub/Sub dan meneruskannya ke Google Chat webhook.

## Build & Run

### Local Development
```bash
go run main.go
```

### Docker Build
```bash
# Build image
docker build -t mtr-pose-receiver .

# Run container
docker run -d \
  --name mtr-pose-receiver \
  -v $(pwd)/service-account.json:/app/service-account.json:ro \
  --env-file .env \
  mtr-pose-receiver
```

### Docker Compose
```bash
# Start service
docker-compose up -d

# View logs
docker-compose logs -f

# Stop service
docker-compose down
```

## Configuration

Copy `.env.example` to `.env` and update the values:

```bash
cp .env.example .env
```

Required environment variables:
- `PROJECT_ID`: Google Cloud Project ID
- `SUBSCRIPTION_ID`: Pub/Sub Subscription ID
- `GOOGLE_APPLICATION_CREDENTIALS`: Path to service account JSON
- `GOOGLE_CHAT_WEBHOOK`: Google Chat webhook URL

## Docker Image Size

Multistage build menghasilkan image yang sangat kecil (~15-20MB) karena:
- Build stage menggunakan `golang:1.24-alpine`
- Runtime stage menggunakan `alpine:latest`
- Binary di-compile dengan flag `-ldflags="-w -s"` untuk strip debug info
- Hanya binary dan ca-certificates yang ada di final image
