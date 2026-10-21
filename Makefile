.PHONY: all build test ui gen dev integration gen-check

all: build

# Reinstall only when the lockfile changes; npm ci deletes node_modules, so
# running it on every build turns a UI iteration into a full reinstall.
UI_DEPS_STAMP := ui/node_modules/.hayduk-deps

$(UI_DEPS_STAMP): ui/package.json ui/package-lock.json
	cd ui && npm ci
	@touch $(UI_DEPS_STAMP)

ui: $(UI_DEPS_STAMP)
	cd ui && npm run build
	rm -rf internal/server/dist
	cp -r ui/dist internal/server/dist

build: ui
	CGO_ENABLED=0 go build -trimpath -o bin/hayduk ./cmd/hayduk

test:
	go test ./...
	cd ui && npm test

gen:
	tygo generate

dev:
	go run ./cmd/hayduk --dev

integration:
	RUN_MSF_INTEGRATION=1 go test -tags integration ./...

gen-check: gen
	git diff --exit-code ui/src/protocol/types.ts
