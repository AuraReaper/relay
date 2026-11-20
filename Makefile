.PHONY: run test race proto

run:
	go mod tidy
	go vet ./...
	go run ./cmd/relayd

test:
	go test ./...

race:
	go test -race ./...

proto:
	protoc \
        --proto_path=. \
        --go_out=. \
        --go-grpc_out=. \
        --go_opt=paths=source_relative \
        --go-grpc_opt=paths=source_relative \
        api/v1/*.proto
