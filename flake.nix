{
  inputs = {
    flake-utils.url = "github:numtide/flake-utils";
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-24.05";
  };

  outputs = {
    self,
    flake-utils,
    nixpkgs,
  }:
    flake-utils.lib.eachDefaultSystem (system: let
      pkgs = import nixpkgs {
        inherit system;
         config.allowUnfree = true;
      };
    in {
      devShell = pkgs.mkShell {
        # NOTE: really useful to debug with `delve`.
        hardeningDisable = [ "fortify" ];
        nativeBuildInputs = with pkgs; [
          # Golang & build tools
          gnumake
          go
          golangci-lint
          gopls
          delve
        ];
        PKG_CONFIG_PATH = "${pkgs.openssl.dev}/lib/pkgconfig";
        # Default database URL for local development.
        DATABASE_URL = "postgresql://postgres:postgres@127.0.0.1:5432/advertiser?sslmode=disable";
      };
    });
}
