build:
	go build -o bin/shimmy-sandbox ./cmd/shimmy-sandbox

test:
	go test ./...

lambda-layer:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/shimmy-sandbox-linux-amd64 ./cmd/shimmy-sandbox
	zip -j lambda-layer.zip bin/shimmy-sandbox-linux-amd64
