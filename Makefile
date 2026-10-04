.PHONY: build test check demo demo-showcase e2e-release
build:
	go build -trimpath -o bin/mirr ./cmd/mirr
test:
	go test -race ./...
check: test
	go vet ./...
demo: build
	./scripts/demo.sh
demo-showcase: build
	./scripts/demo-showcase.sh
e2e-release: build
	./scripts/e2e-release.sh
