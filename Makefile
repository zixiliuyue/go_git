.PHONY: all build test difftest bench verify sbom provenance verify-provenance clean

BIN_DIR := bin
BIN_NAME := gogit
TARGET := $(BIN_DIR)/$(BIN_NAME)

all: build

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(TARGET) ./cmd/gogit
	@echo "Build complete: $(TARGET)"

test:
	go test -v ./...

difftest:
	go test -v ./test/differential/...

bench:
	./bench/perf.sh

verify:
	./test/verify_all.sh

sbom:
	./scripts/generate_sbom.sh

provenance: build
	./scripts/generate_provenance.sh
	./scripts/generate_sbom.sh

verify-provenance: provenance
	./scripts/verify_provenance.sh

clean:
	rm -rf $(BIN_DIR) build /tmp/gogit
