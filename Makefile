.PHONY: build clean stop start restart test

build:
	go build -o newsfornerds ./cmd/srv

clean:
	rm -f newsfornerds

test:
	go test ./...
