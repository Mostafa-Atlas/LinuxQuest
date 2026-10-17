BINARY := bin/linuxquest

.PHONY: all deps build run test vet fmt clean docker-build up down logs

all: test build

# go.sum is generated on first use; commit it afterwards.
go.sum: go.mod
	go mod tidy

deps: go.sum

build: deps
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BINARY) ./cmd/linuxquest

run: deps
	LINUXQUEST_PORT=$${LINUXQUEST_PORT:-2222} go run ./cmd/linuxquest

test: deps
	go test ./...

vet: deps
	go vet ./...

fmt:
	gofmt -l -w .

clean:
	rm -rf bin data

docker-build:
	docker compose build

up:
	docker compose up -d --build

down:
	docker compose down

logs:
	docker compose logs -f linuxquest
