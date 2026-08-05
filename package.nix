{ lib, buildGoModule }:

buildGoModule {
  pname = "codex-history";
  version = "0.5.0";

  src = ./.;
  vendorHash = "sha256-d9F5XPQBE8Sl5bUGvH7i+/ekx9SrjcZPmPKATxtV8zs=";

  subPackages = [ "cmd/codex-history" ];

  ldflags = [
    "-s"
    "-w"
    "-X github.com/HizKz/codex-history/internal/buildinfo.Version=0.5.0"
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
