{
  description = "herdr plugin: rename worktree workspaces to '<ISSUE-ID> <Linear issue title>'";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "aarch64-darwin"
        "x86_64-darwin"
        "aarch64-linux"
        "x86_64-linux"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in
    {
      packages = forAllSystems (pkgs: {
        default = pkgs.buildGoModule {
          pname = "herdr-linear-title";
          version = "0.1.0";
          src = pkgs.lib.fileset.toSource {
            root = ./.;
            fileset = pkgs.lib.fileset.unions [
              ./go.mod
              ./go.sum
              ./cmd
              ./internal
            ];
          };
          vendorHash = "sha256-k3NBfC6PQIzPRNexWofyJKeDDKKq76GxSIA5LvSyeuE=";
          subPackages = [ "cmd/herdr-linear-title" ];
          env.CGO_ENABLED = "0";
          # Tests bind loopback ports, which the darwin build sandbox denies.
          # They run with `make test` instead.
          doCheck = false;
          ldflags = [
            "-s"
            "-w"
          ];
          meta.mainProgram = "herdr-linear-title";
        };
      });

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = with pkgs; [
            go
            gopls
            gotools
            golangci-lint
            gnumake
          ];
        };
      });

      formatter = forAllSystems (pkgs: pkgs.nixfmt);
    };
}
