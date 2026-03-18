.PHONY: build test vet clean

BINARY=servercore-billing-exporter

build:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BINARY) .

test:
	go test -v -race -count=1 ./...

vet:
	go vet ./...

clean:
	rm -f $(BINARY)

docker-build:
	docker build -t $(BINARY) .

docker-run:
	docker run --rm -p 9876:9876 -e TOKEN=$(TOKEN) $(BINARY)
