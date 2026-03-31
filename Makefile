build-app:
	@go build -o test/phantom cmd/main.go
	@echo "Done!"

docker-build:
	@sudo docker build -t peer-phantom .
	@echo "Done!"

docker-run:
	@ADVERTISE_IP="$$(ip route get 1.1.1.1 | awk '{for(i=1;i<=NF;i++) if($$i=="src") {print $$(i+1); exit}}')"; \
	echo "Using ADVERTISE_IP=$$ADVERTISE_IP"; \
	sudo docker run -it --rm --name peer-phantom \
		-p 4001:4001 \
		-e ADVERTISE_IP="$$ADVERTISE_IP" \
		peer-phantom
