{ lib, buildGoModule }:

buildGoModule {
  pname = "codex-history";
  version = "0.4.0";

  src = ./.;
  vendorHash = "sha256-eA9P3TJHxwmfgmeqpj0SufJx/p2Bt+gAC0+Q/TwSetE=";

  subPackages = [ "cmd/codex-history" ];

  ldflags = [
    "-s"
    "-w"
    "-X github.com/HizKz/codex-history/internal/buildinfo.Version=0.4.0"
  ];

  doCheck = true;

  meta = {
    description = "Browse, search, and resume local Codex conversations";
    homepage = "https://github.com/HizKz/codex-history";
    license = lib.licenses.mit;
    mainProgram = "codex-history";
    platforms = lib.platforms.unix;
  };
}
