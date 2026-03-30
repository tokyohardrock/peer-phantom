build-app:
	@go build -o test/phantom cmd/main.go
	@echo "Done!"

docker-build:
	@sudo docker build --no-cache -t peer-phantom .
	@echo "Done!"

docker-run:
	@sudo docker run -it -p 4001:4001 peer-phantom
	@echo "Done!"
