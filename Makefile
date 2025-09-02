.PHONY: build install clean test

BINARY_NAME=zfsrabbit
INSTALL_PATH=/usr/local/bin
CONFIG_PATH=/etc/zfsrabbit
SERVICE_PATH=/etc/systemd/system

build:
	go build -o $(BINARY_NAME) .

install: build
	sudo mkdir -p $(CONFIG_PATH)
	sudo cp $(BINARY_NAME) $(INSTALL_PATH)/
	sudo cp config.yaml.example $(CONFIG_PATH)/
	sudo cp zfsrabbit.service $(SERVICE_PATH)/
	sudo systemctl daemon-reload
	@echo "Installation complete. Edit $(CONFIG_PATH)/config.yaml and set ZFSRABBIT_ADMIN_PASSWORD"

uninstall:
	sudo systemctl stop zfsrabbit || true
	sudo systemctl disable zfsrabbit || true
	sudo rm -f $(INSTALL_PATH)/$(BINARY_NAME)
	sudo rm -f $(SERVICE_PATH)/zfsrabbit.service
	sudo systemctl daemon-reload
	@echo "Uninstallation complete. Configuration files left in $(CONFIG_PATH)"

start:
	sudo systemctl start zfsrabbit

stop:
	sudo systemctl stop zfsrabbit

restart:
	sudo systemctl restart zfsrabbit

status:
	sudo systemctl status zfsrabbit

logs:
	sudo journalctl -u zfsrabbit -f

enable:
	sudo systemctl enable zfsrabbit

disable:
	sudo systemctl disable zfsrabbit

clean:
	rm -f $(BINARY_NAME)

test:
	go test ./...

dev:
	go run . -config config.yaml.example