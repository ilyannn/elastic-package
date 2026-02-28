version_import_path := "github.com/elastic/elastic-package/internal/version"
commit_hash := `git describe --always --long --dirty`
build_time := `date +%s`
version_tag := `(git describe --exact-match --tags 2>/dev/null || echo '') | tr -d '\n'`
ldflags := "-X " + version_import_path + ".CommitHash=" + commit_hash + " -X " + version_import_path + ".BuildTime=" + build_time + " -X " + version_import_path + ".Tag=" + version_tag

build:
    go build -ldflags "{{ldflags}}" -o elastic-package

install: build
    mkdir -p ~/.local/bin
    cp elastic-package ~/.local/bin/

check: build format lint licenser gomod update
    git update-index --really-refresh
    git diff-index --quiet HEAD

test:
    mkdir -p build/test-coverage
    go run gotest.tools/gotestsum --format standard-verbose -- -count 1 -coverprofile=build/test-coverage/coverage-unit-report.out ./...

format:
    go run golang.org/x/tools/cmd/goimports -local github.com/elastic/elastic-package/ -w .

lint:
    go run honnef.co/go/tools/cmd/staticcheck ./...

licenser:
    go run github.com/elastic/go-licenser -license Elastic

gomod:
    go mod tidy

update:
    cd tools/readme && go run main.go

clean:
    rm -rf build
    rm -f elastic-package

# Run tests for specific packages: just test-pkg ./internal/packages/...
test-pkg *args:
    go test {{args}}

# Run the tool directly: just run stack up -v -d
run *args:
    go run . {{args}}

# Integration test a specific test package: just test-integration ./test/packages/parallel/apache
test-integration dir:
    go run . test -C {{dir}}
