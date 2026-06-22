BINARY_PATH ?= .
build-app:
	@go build -o $(BINARY_PATH) cmd/main.go
	@echo "Done!"

docker-build:
	@sudo docker build -t peer-phantom .
	@echo "Done!"

PORT ?= 4001
docker-run:
	@IP="$$(ip route get 1.1.1.1 | awk '{for(i=1;i<=NF;i++) if($$i=="src") {print $$(i+1); exit}}')"; \
	echo "Using IP=$$IP"; \
	sudo docker run -it --rm --name peer-phantom \
		-p $(PORT):$(PORT) \
		-e IP="$$IP" \
		-e PORT="$(PORT)" \
		peer-phantom
