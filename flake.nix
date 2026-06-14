{
  description = "gigahost-go: Go API client, CLI and Terraform provider for gigahost.no";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    flake-checks.url = "github:kradalby/flake-checks";
    flake-checks.inputs.nixpkgs.follows = "nixpkgs";
  };

  outputs =
    { self
    , nixpkgs
    , flake-utils
    , flake-checks
    , ...
    }:
    let
      version = self.shortRev or self.dirtyShortRev or "dev";
      commitHash = self.rev or self.dirtyRev or "dirty";
    in
    {
      overlays.default = _: prev:
        let
          pkgs = nixpkgs.legacyPackages.${prev.stdenv.hostPlatform.system};
          buildGo = pkgs.buildGo126Module;
          # Module vendor hashes (no in-tree vendor dir). Recompute after go.mod
          # / go.sum changes. The provider is a nested module that replaces the
          # parent (=> ../), so it is built from the repo root via modRoot.
          rootVendorHash = "sha256-VTczZxefrgQBNFYiODY08xQRrD24ZJFyuEsRuIdB0vE=";
          providerVendorHash = "sha256-JdfFnGNWHs1Xanle56Gc3/Rgj0TDeNONCHz28Y898mk=";
        in
        {
          gigahost = buildGo {
            pname = "gigahost";
            inherit version;
            src = pkgs.lib.cleanSource self;

            subPackages = [ "cmd/gigahost" ];

            # The repo is a go workspace; vendoring must not run in workspace mode.
            env.GOWORK = "off";
            vendorHash = rootVendorHash;

            ldflags = [
              "-s"
              "-w"
              "-X main.version=${version}"
              "-X main.commit=${commitHash}"
            ];

            # Only run unit tests when testing a build.
            checkFlags = [ "-short" ];

            meta = {
              description = "Go API client and CLI for gigahost.no";
              homepage = "https://github.com/kradalby/gigahost-go";
              license = pkgs.lib.licenses.mit;
              mainProgram = "gigahost";
            };
          };

          terraform-provider-gigahost = buildGo {
            pname = "terraform-provider-gigahost";
            inherit version;
            # The Terraform provider lives in its own Go module inside a
            # subdirectory so it can be split off to its own repository for
            # Terraform Registry publishing. We build from that subdirectory
            # using the local workspace.
            src = pkgs.lib.cleanSource self;
            modRoot = "terraform-provider-gigahost";

            env.GOWORK = "off";
            vendorHash = providerVendorHash;

            ldflags = [
              "-s"
              "-w"
              "-X main.version=${version}"
              "-X main.commit=${commitHash}"
            ];

            checkFlags = [ "-short" ];

            meta = {
              description = "Terraform provider for gigahost.no";
              homepage = "https://github.com/kradalby/terraform-provider-gigahost";
              license = pkgs.lib.licenses.mpl20;
              mainProgram = "terraform-provider-gigahost";
            };
          };

          # Pin golangci-lint against Go 1.26 (upstream hardcodes its Go toolchain).
          golangci-lint = buildGo rec {
            pname = "golangci-lint";
            version = "2.11.4";

            src = pkgs.fetchFromGitHub {
              owner = "golangci";
              repo = "golangci-lint";
              rev = "v${version}";
              hash = "sha256-B19aLvfNRY9TOYw/71f2vpNUuSIz8OI4dL0ijGezsas=";
            };

            vendorHash = "sha256-xuoj4+U4tB5gpABKq4Dbp2cxnljxdYoBbO8A7DqPM5E=";

            subPackages = [ "cmd/golangci-lint" ];

            nativeBuildInputs = [ pkgs.installShellFiles ];

            ldflags = [
              "-s"
              "-w"
              "-X main.version=${version}"
              "-X main.commit=v${version}"
              "-X main.date=1970-01-01T00:00:00Z"
            ];

            postInstall = ''
              for shell in bash zsh fish; do
                HOME=$TMPDIR $out/bin/golangci-lint completion $shell > golangci-lint.$shell
                installShellCompletion golangci-lint.$shell
              done
            '';

            meta = {
              description = "Fast linters runner for Go";
              homepage = "https://golangci-lint.run/";
              mainProgram = "golangci-lint";
            };
          };

          # Re-build common Go dev tools against Go 1.26 so everything agrees on
          # a single Go version.
          gotestsum = prev.gotestsum.override { buildGoModule = buildGo; };
          gotests = prev.gotests.override { buildGoModule = buildGo; };
          gofumpt = prev.gofumpt.override { buildGoModule = buildGo; };
          gopls = prev.gopls.override { buildGoLatestModule = buildGo; };
        };
    }
    // flake-utils.lib.eachDefaultSystem
      (system:
      let
        pkgs = import nixpkgs {
          overlays = [ self.overlays.default ];
          inherit system;
        };

        # flake-checks: cache-friendly Go gate checks. The repo is two Go
        # modules — the root API client/CLI, and a nested Terraform provider
        # in its own module — so each gets its own `common` and distinct
        # check names.
        fc = flake-checks.lib;

        common = {
          inherit pkgs version;
          root = ./.;
          pname = "gigahost";
          vendorHash = "sha256-VTczZxefrgQBNFYiODY08xQRrD24ZJFyuEsRuIdB0vE=";
          goPkg = pkgs.go_1_26;
          # client/*_test.go decode fixtures from client/testdata.
          extraSrc = [ ./client/testdata ];
          # The nested provider is its own Go module — keep it out of the root.
          excludeSrc = [ ./terraform-provider-gigahost ];
        };

        buildDeps = with pkgs; [ git go_1_26 gnumake ];

        devDeps = with pkgs;
          buildDeps
          ++ [
            # Go tooling built against Go 1.26
            golangci-lint
            gofumpt
            gopls
            gotestsum
            gotests
            goreleaser

            # Formatters / pre-commit stack
            prek
            prettier
            nixpkgs-fmt
            python314Packages.mdformat

            # OpenTofu is the primary driver for local provider
            # development. The Terraform binary is closed-source
            # (BUSL-1.1); OpenTofu is a compatible MPL-licensed fork and
            # speaks the same plugin protocol, so the provider works
            # unmodified against either.
            opentofu
            # tfplugindocs generates Registry-compatible provider docs;
            # the output works for both the Terraform and OpenTofu
            # registries.
            terraform-plugin-docs

            # Utilities
            ripgrep
            jq
            yq-go
            graphviz
          ];
      in
      {
        devShells.default = pkgs.mkShell {
          buildInputs =
            devDeps
            ++ [
              # Helper: recompute vendor sha for buildGoModule.
              (pkgs.writeShellScriptBin
                "nix-vendor-sri"
                ''
                  set -euo pipefail
                  OUT=$(mktemp -d -t nar-hash-XXXXXX)
                  trap 'rm -rf "$OUT"' EXIT
                  go mod vendor -o "$OUT"
                  ${pkgs.nix}/bin/nix hash path --type sha256 --sri "$OUT"
                '')

              # Helper: bulk-upgrade direct module deps.
              (pkgs.writeShellScriptBin
                "go-mod-update-all"
                ''
                  set -euo pipefail
                  ${pkgs.ripgrep}/bin/rg '^\t' go.mod | ${pkgs.ripgrep}/bin/rg -v indirect | ${pkgs.gawk}/bin/awk '{print $1}' | ${pkgs.findutils}/bin/xargs go get -u
                  go mod tidy
                '')
            ];

          shellHook = ''
            export PATH="$PWD/result/bin:$PATH"
            export CGO_ENABLED=0
          '';
        };

        formatter = fc.formatter common;

        # Go CI gate, one job per check (see .github/workflows). Root module
        # and nested provider module get distinct names.
        # Root module gets the full lib gate. The nested provider is a separate
        # module that replaces the parent (=> ../) under a go.work, so it can't
        # use the single-root lib checks — gate its compile via its package
        # (built with modRoot + GOWORK=off); root `formatting` covers its *.go.
        checks = {
          build = fc.goBuild common;
          gotest = fc.goTest (common // { goRace = true; });
          golangci-lint = fc.goLint common;
          formatting = fc.goFormat common;
          build-provider = pkgs.terraform-provider-gigahost;
        };

        packages = {
          inherit (pkgs) gigahost terraform-provider-gigahost;
          default = pkgs.gigahost;
        };

        # Workflow apps replace the Makefile: `nix run .#test`, `.#lint`, etc.
        # Each runs in the caller's working directory with the pinned dev
        # toolchain on PATH, so they need no `nix develop` wrapper.
        apps =
          let
            binPath = pkgs.lib.makeBinPath devDeps;
            mkApp = name: text:
              flake-utils.lib.mkApp {
                drv = pkgs.writeShellScriptBin name ''
                  set -euo pipefail
                  export PATH="${binPath}:$PATH"
                  export CGO_ENABLED=0
                  ${text}
                '';
              };
          in
          {
            gigahost = flake-utils.lib.mkApp { drv = pkgs.gigahost; };
            terraform-provider-gigahost = flake-utils.lib.mkApp {
              drv = pkgs.terraform-provider-gigahost;
            };
            default = flake-utils.lib.mkApp { drv = pkgs.gigahost; };

            test = mkApp "test" ''
              go test -race -short ./...
              (cd terraform-provider-gigahost && go test -race -short ./...)
            '';

            test-acc = mkApp "test-acc" ''
              export TF_ACC=1
              export TF_ACC_TERRAFORM_PATH="$(command -v tofu)"
              export TF_ACC_PROVIDER_NAMESPACE=hashicorp
              export TF_ACC_PROVIDER_HOST=registry.opentofu.org
              go test -v -timeout 30m ./tfprovider/...
            '';

            test-e2e = mkApp "test-e2e" ''
              go test -tags e2e -v -timeout 30m ./e2e/... ./cli/...
            '';

            lint = mkApp "lint" ''
              golangci-lint run --timeout=10m ./...
              (cd terraform-provider-gigahost && golangci-lint run --timeout=10m ./...)
            '';

            fmt = mkApp "fmt" ''
              gofumpt -w .
              tofu fmt -recursive terraform-provider-gigahost/examples
              golangci-lint run --fix --timeout=10m ./... || true
              (cd terraform-provider-gigahost && golangci-lint run --fix --timeout=10m ./... || true)
            '';

            tidy = mkApp "tidy" ''
              go mod tidy
              (cd terraform-provider-gigahost && go mod tidy)
            '';

            # tfplugindocs 0.24 only knows how to download Terraform (which
            # fails: expired signing key, and we ship OpenTofu). So export the
            # schema with OpenTofu via a dev-override and feed it in.
            tfdocs = mkApp "tfdocs" ''
              root="$PWD"
              tmp="$(mktemp -d)"
              trap 'rm -rf "$tmp"' EXIT
              (cd "$root/terraform-provider-gigahost" && go build -o "$tmp/terraform-provider-gigahost" .)
              cat > "$tmp/dev.tfrc" <<EOF
              provider_installation {
                dev_overrides { "registry.terraform.io/hashicorp/gigahost" = "$tmp" }
                direct {}
              }
              EOF
              mkdir -p "$tmp/cfg"
              cat > "$tmp/cfg/main.tf" <<EOF
              terraform {
                required_providers {
                  gigahost = {
                    source = "registry.terraform.io/hashicorp/gigahost"
                  }
                }
              }

              provider "gigahost" {}
              EOF
              (cd "$tmp/cfg" && TF_CLI_CONFIG_FILE="$tmp/dev.tfrc" tofu providers schema -json > "$tmp/schema.json" 2>/dev/null)
              (cd "$root/terraform-provider-gigahost" && tfplugindocs generate \
                --provider-name gigahost \
                --rendered-provider-name Gigahost \
                --providers-schema "$tmp/schema.json")
            '';

            generate = mkApp "generate" ''
              go generate ./...
            '';
          };
      });
}
