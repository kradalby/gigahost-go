{
  description = "gigahost-go: Go API client, CLI and Terraform provider for gigahost.no";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    flake-checks.url = "github:kradalby/flake-checks";
    flake-checks.inputs.nixpkgs.follows = "nixpkgs";
    flake-checks.inputs.flake-utils.follows = "flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
      flake-checks,
      ...
    }:
    let
      version = self.shortRev or self.dirtyShortRev or "dev";
      commitHash = self.rev or self.dirtyRev or "dirty";
      # Root module vendor hash. Shared between the overlay package and the
      # flake-checks `common` so it lives in one place. Recompute after
      # go.mod / go.sum changes (`nix-vendor-sri` in the devShell).
      rootVendorHash = "sha256-yIB9is+pLnCYlqj/dhYRR3o3IWFzyzPm9SJ0T2qi7s4=";
    in
    {
      overlays.default =
        _: prev:
        let
          pkgs = nixpkgs.legacyPackages.${prev.stdenv.hostPlatform.system};
          buildGo = pkgs.buildGoLatestModule;
          # Provider vendor hash (no in-tree vendor dir). The provider is a
          # nested module that replaces the parent (=> ../), so its vendor tree
          # contains the root module's own *.go files: recompute after any root
          # Go source change, not just go.mod / go.sum. Take the value from the
          # `got:` line of a deliberate `nix build .#terraform-provider-gigahost`
          # mismatch — nix's cleanSource copy hashes differently from a local
          # `go mod vendor`. Built from the repo root via modRoot. The root
          # module hash is hoisted to the top-level `rootVendorHash`.
          providerVendorHash = "sha256-eb48JI+BCeQlxotFreWOPLASWABpRSVVN8b1dzcXzm0=";
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

            # The offline suite is the whole test suite here: the acceptance
            # and e2e tests self-skip without TF_ACC and a token.
            checkFlags = [ ];

            meta = {
              description = "Go API client and CLI for gigahost.no";
              homepage = "https://github.com/kradalby/gigahost-go";
              license = pkgs.lib.licenses.bsd3;
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

            checkFlags = [ ];

            meta = {
              description = "Terraform provider for gigahost.no";
              homepage = "https://github.com/kradalby/terraform-provider-gigahost";
              license = pkgs.lib.licenses.bsd3;
              mainProgram = "terraform-provider-gigahost";
            };
          };

          # Re-build common Go dev tools against the latest Go so everything
          # agrees on a single Go version. golangci-lint and gopls already
          # track it upstream, so they need no override.
          gotestsum = prev.gotestsum.override { buildGoModule = buildGo; };
          gotests = prev.gotests.override { buildGoModule = buildGo; };
          gofumpt = prev.gofumpt.override { buildGoModule = buildGo; };
          gotools = prev.gotools.override { buildGoModule = buildGo; };
        };
    }
    # Not eachDefaultSystem: it still lists x86_64-darwin, which nixpkgs
    # 26.11 dropped, so evaluating any output for it throws.
    // flake-utils.lib.eachSystem [ "x86_64-linux" "aarch64-linux" "aarch64-darwin" ] (
      system:
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
          vendorHash = rootVendorHash;
          goPkg = pkgs.go_latest;
          # client/*_test.go decode fixtures from client/testdata.
          extraSrc = [ ./client/testdata ];
          # The nested provider is its own Go module — keep it out of the root.
          excludeSrc = [ ./terraform-provider-gigahost ];
        };

        buildDeps = with pkgs; [
          git
          go_latest
        ];

        devDeps =
          with pkgs;
          buildDeps
          ++ [
            # Go tooling built against the latest Go
            golangci-lint
            gofumpt
            gopls
            gotestsum
            gotests
            goreleaser

            # Formatters / pre-commit stack
            prek
            prettier
            nixfmt
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
          buildInputs = devDeps ++ [
            # Helper: recompute vendor sha for buildGoModule.
            (pkgs.writeShellScriptBin "nix-vendor-sri" ''
              set -euo pipefail
              # The repo is a go.work workspace; `go mod vendor` refuses to
              # run in workspace mode, and the root hash is the root module.
              export GOWORK=off
              OUT=$(mktemp -d -t nar-hash-XXXXXX)
              trap 'rm -rf "$OUT"' EXIT
              go mod vendor -o "$OUT"
              ${pkgs.nix}/bin/nix hash path --type sha256 --sri "$OUT"
            '')

            # Helper: bulk-upgrade direct module deps.
            (pkgs.writeShellScriptBin "go-mod-update-all" ''
              set -euo pipefail
              ${pkgs.ripgrep}/bin/rg '^\t' go.mod | ${pkgs.ripgrep}/bin/rg -v indirect | ${pkgs.gawk}/bin/awk '{print $1}' | ${pkgs.findutils}/bin/xargs go get -u
              go mod tidy
            '')
          ];

          shellHook = ''
            export PATH="$PWD/result/bin:$PATH"
            export CGO_ENABLED=0
            # Never fetch a toolchain. A go.mod ahead of nixpkgs' Go must be a
            # clear error, not a silent download from go.dev outside the store.
            export GOTOOLCHAIN=local
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
            # `nix flake check` warns about apps without a meta.description.
            mkApp =
              name: description: text:
              flake-utils.lib.mkApp {
                drv = pkgs.writeShellScriptBin name ''
                  set -euo pipefail
                  export PATH="${binPath}:$PATH"
                  export CGO_ENABLED=0
                  export GOTOOLCHAIN=local
                  ${text}
                '';
              }
              // {
                meta = { inherit description; };
              };
            pkgApp =
              drv: flake-utils.lib.mkApp { inherit drv; } // { meta = { inherit (drv.meta) description; }; };
          in
          {
            gigahost = pkgApp pkgs.gigahost;
            terraform-provider-gigahost = pkgApp pkgs.terraform-provider-gigahost;
            default = pkgApp pkgs.gigahost;

            test = mkApp "test" "Run the unit tests of both modules with -race" ''
              go test -race ./...
              (cd terraform-provider-gigahost && go test -race ./...)
            '';

            test-acc = mkApp "test-acc" "Run the provider acceptance tests against the live API" ''
              export TF_ACC=1
              export TF_ACC_TERRAFORM_PATH="$(command -v tofu)"
              export TF_ACC_PROVIDER_NAMESPACE=hashicorp
              export TF_ACC_PROVIDER_HOST=registry.opentofu.org
              go test -v -timeout 30m ./tfprovider/...
            '';

            test-e2e = mkApp "test-e2e" "Run the live end-to-end suites" ''
              go test -tags e2e -v -timeout 30m ./e2e/... ./cli/...
            '';

            lint = mkApp "lint" "Run golangci-lint on both modules" ''
              golangci-lint run --timeout=10m ./...
              (cd terraform-provider-gigahost && golangci-lint run --timeout=10m ./...)
            '';

            fmt = mkApp "fmt" "Format Go sources and Terraform examples" ''
              gofumpt -w .
              tofu fmt -recursive terraform-provider-gigahost/examples
              golangci-lint run --fix --timeout=10m ./... || true
              (cd terraform-provider-gigahost && golangci-lint run --fix --timeout=10m ./... || true)
            '';

            tidy = mkApp "tidy" "Run go mod tidy on both modules" ''
              go mod tidy
              (cd terraform-provider-gigahost && go mod tidy)
            '';

            # tfplugindocs only knows how to download Terraform, and we ship
            # OpenTofu. So export the schema with OpenTofu via a dev-override
            # and feed it in.
            tfdocs = mkApp "tfdocs" "Regenerate the provider registry docs" ''
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

            generate = mkApp "generate" "Run go generate" ''
              go generate ./...
            '';
          };
      }
    );
}
