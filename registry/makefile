SOURCE_FILE=zot-2.0.4.tar.gz
EXTRACTED_DIR=zot-2.0.4
BIN_DIR=../bin
BIN=bin
ZOT_BINARY=zot
ZLI_BINARY=zli

# Build calls the appropriate build function based on the OS
build:
	@OS=$(shell uname | tr '[:upper:]' '[:lower:]'); \
	ARCH=$(shell uname -m); \
	if [ "$$OS" = "darwin" ] && [ "$$ARCH" = "arm64" ]; then \
		echo "Building for macOS ARM..."; \
		$(MAKE) _build ZOT_BINARY=zot-darwin-arm64 ZLI_BINARY=zli-darwin-arm64; \
	elif [ "$$OS" = "darwin" ] && [ "$$ARCH" = "x86_64" ]; then \
		echo "Building for macOS Intel..."; \
		$(MAKE) _build ZOT_BINARY=zot-darwin-amd64 ZLI_BINARY=zli-darwin-amd64; \
	elif [ "$$OS" = "linux" ] && [ "$$ARCH" = "arm64" ]; then \
		echo "Building for Linux ARM..."; \
		$(MAKE) _build ZOT_BINARY=zot-linux-arm64 ZLI_BINARY=zli-linux-arm64; \
	elif [ "$$OS" = "linux" ] && [ "$$ARCH" = "x86_64" ]; then \
		echo "Building for Linux Intel..."; \
		$(MAKE) _build ZOT_BINARY=zot-linux-amd64 ZLI_BINARY=zli-linux-amd64; \
	else \
		echo "Unsupported OS."; \
		exit 1; \
	fi

_build:
	@test -d $(BIN) || mkdir -p $(BIN)
	@if [ ! -f $(BIN)/$(ZOT_BINARY) ]; then \
		tar -xzvf $(SOURCE_FILE); \
		cd $(EXTRACTED_DIR) && make binary ; \
		cp $(BIN)/$(ZOT_BINARY) ../$(BIN)/$(ZOT_BINARY); \
		cp ../config.json ../$(BIN)/config.json; \
		rm -rf ../$(EXTRACTED_DIR); \
		echo "---------------------------------------------------------------------------" ; \
	else \
		echo "$(ZOT_BINARY) already exists, skipping build."; \
		echo "---------------------------------------------------------------------------" ; \
	fi
	
	@if [ ! -f $(BIN)/$(ZLI_BINARY) ]; then \
		tar -xzvf $(SOURCE_FILE); \
		cd $(EXTRACTED_DIR) && make cli; \
		cp $(BIN)/$(ZLI_BINARY) ../$(BIN)/$(ZLI_BINARY); \
		cp ../config.json ../$(BIN)/config.json; \
		rm -rf ../$(EXTRACTED_DIR); \
	else \
		echo "$(ZLI_BINARY) already exists, skipping build."; \
	fi

run:
	@OS=$(shell uname | tr '[:upper:]' '[:lower:]'); \
	ARCH=$(shell uname -m); \
	if [ "$$OS" = "darwin" ] && [ "$$ARCH" = "arm64" ]; then \
		echo "Running for MacOS ARM..."; \
		$(BIN)/zot-darwin-arm64 serve ./config.json; \
	elif [ "$$OS" = "darwin" ] && [ "$$ARCH" = "x86_64" ]; then \
		echo "Running for MacOS Intel..."; \
		$(BIN)/zot-darwin-amd64 serve ./config.json; \
	elif [ "$$OS" = "linux" ] && [ "$$ARCH" = "arm64" ]; then \
		echo "Running for Linux ARM..."; \
		$(BIN)/zot-linux-arm64 serve ./config.json; \
	elif [ "$$OS" = "linux" ] && [ "$$ARCH" = "x86_64" ]; then \
		echo "Running for Linux Intel..."; \
		$(BIN)/zot-linux-amd64 serve ./config.json; \
	else \
		echo "Unsupported OS or architecture."; \
		exit 1; \
	fi

clean:
	rm -rf $(EXTRACTED_DIR) $(BIN)


