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
        in
        {
          gigahost = buildGo {
            pname = "gigahost";
            inherit version;
            src = pkgs.lib.cleanSource self;

            subPackages = [ "cmd/gigahost" ];

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

        # flake-checks: cache-friendly Go gate checks.
        fc = flake-checks.lib;

        common = {
          inherit pkgs version;
          root = ./.;
          pname = "gigahost";
          vendorHash = rootVendorHash;
          goPkg = pkgs.go_latest;
          # client/*_test.go decode fixtures from client/testdata.
          extraSrc = [ ./client/testdata ];
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

        # Go CI gate, one job per check (see .github/workflows).
        checks = {
          build = fc.goBuild common;
          gotest = fc.goTest (common // { goRace = true; });
          golangci-lint = fc.goLint common;
          formatting = fc.goFormat common;
        };

        packages = {
          inherit (pkgs) gigahost;
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
            default = pkgApp pkgs.gigahost;

            test = mkApp "test" "Run the unit tests with -race" ''
              go test -race ./...
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

            lint = mkApp "lint" "Run golangci-lint" ''
              golangci-lint run --timeout=10m ./...
            '';

            fmt = mkApp "fmt" "Format Go sources" ''
              gofumpt -w .
              golangci-lint run --fix --timeout=10m ./... || true
            '';

            tidy = mkApp "tidy" "Run go mod tidy" ''
              go mod tidy
            '';

            generate = mkApp "generate" "Run go generate" ''
              go generate ./...
            '';
          };
      }
    );
}
