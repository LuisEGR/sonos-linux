PREFIX      ?= /usr/local
LIBDIR      := $(PREFIX)/lib/sonos-linux
BINDIR      := $(PREFIX)/bin
SYSTEMD_USER := /usr/lib/systemd/user
BINARY      := sonos-linux

.PHONY: build install uninstall enable disable clean

build:
	go build -o $(BINARY) .

install: build
	install -d $(DESTDIR)$(LIBDIR)
	install -m 755 $(BINARY) $(DESTDIR)$(LIBDIR)/$(BINARY)
	install -m 755 tray.py $(DESTDIR)$(LIBDIR)/tray.py
	install -d $(DESTDIR)$(BINDIR)
	ln -sf $(LIBDIR)/$(BINARY) $(DESTDIR)$(BINDIR)/$(BINARY)
	install -d $(DESTDIR)$(SYSTEMD_USER)
	install -m 644 sonos-linux.service $(DESTDIR)$(SYSTEMD_USER)/sonos-linux.service
	@echo ""
	@echo "Installed. Now run (without sudo):"
	@echo "  make enable"

enable:
	systemctl --user daemon-reload
	systemctl --user enable --now sonos-linux

disable:
	systemctl --user disable --now sonos-linux

uninstall:
	-systemctl --user disable --now sonos-linux 2>/dev/null
	rm -f $(DESTDIR)$(BINDIR)/$(BINARY)
	rm -rf $(DESTDIR)$(LIBDIR)
	rm -f $(DESTDIR)$(SYSTEMD_USER)/sonos-linux.service
	-systemctl --user daemon-reload 2>/dev/null

clean:
	rm -f $(BINARY)
