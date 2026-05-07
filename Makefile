.PHONY: build clean test frontend

build: frontend
	go build -o newsfornerds ./cmd/srv

frontend:
	cd frontend && npm run build

clean:
	rm -f newsfornerds
	rm -rf srv/static/dist

test:
	go test ./...
