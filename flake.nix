{
  description = "Igloo server package and OCI container image";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];

      forAllSystems = nixpkgs.lib.genAttrs systems;
      goVersion = "1.26.6";
      goBinaryArchives = {
        x86_64-linux = {
          arch = "amd64";
          hash = "sha256-cI7/t3S+gjdXDQrdFjIlq7369PyiiyYR3xZ766T+74k=";
        };
        aarch64-linux = {
          arch = "arm64";
          hash = "sha256-0FB+np1/4BKq5XAQjL12wV3oeeFxMKuMuQ1NdEXLHy4=";
        };
      };
      goFor =
        system: pkgs:
        let
          upstreamGo = pkgs.go_1_26 or pkgs.go;
          goArchive = goBinaryArchives.${system};
        in
        if (upstreamGo.version or "") == goVersion then
          upstreamGo
        else
          pkgs.stdenvNoCC.mkDerivation {
            pname = "go";
            version = goVersion;

            src = pkgs.fetchurl {
              url = "https://dl.google.com/go/go${goVersion}.linux-${goArchive.arch}.tar.gz";
              hash = goArchive.hash;
            };

            dontConfigure = true;
            dontBuild = true;

            CGO_ENABLED = upstreamGo.CGO_ENABLED or 1;
            GOOS = upstreamGo.GOOS or "linux";
            GOARCH = upstreamGo.GOARCH or goArchive.arch;

            installPhase = ''
              runHook preInstall
              mkdir -p "$out"
              cp -R . "$out"
              runHook postInstall
            '';

            meta = (upstreamGo.meta or { }) // {
              description = "Go compiler and tools";
              homepage = "https://go.dev/";
              license = pkgs.lib.licenses.bsd3;
              platforms = [ system ];
            };
          };
    in
    {
      packages = forAllSystems (
        system:
        let
          pkgs = import nixpkgs {
            inherit system;
          };
          lib = pkgs.lib;
          revision = self.shortRev or (self.dirtyShortRev or "dev");
          containerImageName = "ghcr.io/screwys/igloo";
          go = goFor system pkgs;
          buildGoModule = pkgs.buildGoModule.override { inherit go; };
          pythonPackages = pkgs.python3Packages;
          runtimeRequirementLines = lib.splitString "\n" (builtins.readFile ./requirements-runtime.txt);
          runtimeToolSource =
            package:
            let
              requirementPrefix = "${package} @ ";
              requirementMatches = builtins.filter (
                line: lib.hasPrefix requirementPrefix line
              ) runtimeRequirementLines;
              archiveURL = lib.removePrefix requirementPrefix (builtins.head requirementMatches);
            in
            if builtins.length requirementMatches != 1 then
              throw "expected exactly one ${package} source in requirements-runtime.txt"
            else
              builtins.fetchTarball archiveURL;

          ytDlp = pythonPackages.buildPythonApplication rec {
            pname = "yt-dlp";
            version = "head-${builtins.substring 0 7 (builtins.baseNameOf src)}";
            pyproject = true;

            src = runtimeToolSource "yt-dlp";

            build-system = [
              pythonPackages.hatchling
            ];

            dependencies = [
              pythonPackages.requests
              pythonPackages.curl-cffi
            ];

            doCheck = false;
            pythonImportsCheck = [ "yt_dlp" ];

            meta = {
              description = "Command-line program to download videos";
              homepage = "https://github.com/yt-dlp/yt-dlp";
              license = lib.licenses.unlicense;
              mainProgram = "yt-dlp";
              platforms = lib.platforms.linux;
            };
          };

          galleryDl = pythonPackages.buildPythonApplication rec {
            pname = "gallery_dl";
            version = "head-${builtins.substring 0 7 (builtins.baseNameOf src)}";
            pyproject = true;

            src = runtimeToolSource "gallery-dl";

            build-system = [
              pythonPackages.setuptools
            ];

            dependencies = [
              pythonPackages.requests
            ];

            doCheck = false;
            pythonImportsCheck = [ "gallery_dl" ];

            meta = {
              description = "Command-line program to download image galleries";
              homepage = "https://codeberg.org/mikf/gallery-dl";
              license = lib.licenses.gpl2Only;
              mainProgram = "gallery-dl";
              platforms = lib.platforms.linux;
            };
          };

          pythonWheel =
            pname: version: hash: dependencies:
            pythonPackages.buildPythonPackage {
              inherit pname version dependencies;
              format = "wheel";
              src = pythonPackages.fetchPypi {
                inherit pname version hash;
                format = "wheel";
                dist = "py3";
                python = "py3";
              };
              doCheck = false;
            };

          livePyee = pythonPackages.pyee.overridePythonAttrs (_: {
            version = "13.0.1";
            src = pythonPackages.fetchPypi {
              pname = "pyee";
              version = "13.0.1";
              hash = "sha256-C5MffBRTVmftTH4NUxcWNocV6GC5iHcPx+uFeNH2f8g=";
            };
            doCheck = false;
          });

          liveBetterproto =
            pythonWheel "betterproto2" "0.9.1" "sha256-3gVEtLK2taBc4MG/rDbSMvdFCHsCkH50jzFGa+z8Pb0="
              [
                pythonPackages.python-dateutil
                pythonPackages.typing-extensions
                pythonPackages.pydantic
              ];
          liveProto =
            pythonWheel "tiktokliveproto" "0.2.2" "sha256-070+uuOx7MR9ljzT//w0AwdtEpfxCOWZZCedj1ycCQ4="
              [ liveBetterproto ];
          liveEuler =
            pythonWheel "eulerapisdk" "0.1.0" "sha256-FGAoMWqTizQ6dBMarrGUZ7X0e/Gb1cccAoZNRbEcjO8="
              [
                pythonPackages.attrs
                pythonPackages.httpx
                pythonPackages.python-dateutil
              ];
          liveWebsockets =
            pythonWheel "websockets_proxy" "0.1.3" "sha256-B8oLVhEH5OIHwApchvkoHAW+Kj40qnIdAXQ3YNxf0Gc="
              [
                pythonPackages.python-socks
                pythonPackages.websockets
              ];
          tiktokLive =
            pythonWheel "tiktoklive" "7.0.1" "sha256-20N4nP0quv9jlRfz6V8dN/dbO5EE121wkQKvsRVCCrw="
              [
                pythonPackages.httpx
                livePyee
                pythonPackages.ffmpy
                liveWebsockets
                liveBetterproto
                pythonPackages.async-timeout
                pythonPackages.mashumaro
                pythonPackages.protobuf3-to-dict
                pythonPackages.protobuf
                liveProto
                liveEuler
              ];
          tiktokPython = pkgs.python3.withPackages (_: [ tiktokLive ]);

          sourceRoots = [
            "cmd"
            "internal"
            "locales"
            "static"
          ];

          source = lib.cleanSourceWith {
            src = ./.;
            filter =
              path: _type:
              let
                root = toString ./.;
                rel = lib.removePrefix (root + "/") (toString path);
              in
              rel == ""
              || rel == "go.mod"
              || rel == "go.sum"
              || lib.any (prefix: rel == prefix || lib.hasPrefix "${prefix}/" rel) sourceRoots;
          };

          igloo = buildGoModule {
            pname = "igloo";
            version = "0.0.0-${revision}";

            src = source;
            vendorHash = "sha256-2lG3Bv4dlQOgxIpF2e8UTTMwFhbEvEPb6R+LWfz/Ydk=";

            subPackages = [
              "cmd/igloo"
              "cmd/adduser"
            ];

            ldflags = [
              "-s"
              "-w"
            ];

            nativeBuildInputs = [ pkgs.makeWrapper ];

            postBuild = ''
              go run ./cmd/igloo-assets
            '';

            overrideModAttrs = _: {
              postBuild = "";
            };

            postInstall = ''
              mv "$out/bin/adduser" "$out/bin/igloo-adduser"
              mkdir -p "$out/share/igloo"
              cp -R static locales "$out/share/igloo/"
              wrapProgram "$out/bin/igloo" --prefix PATH : "${lib.getBin pkgs.postgresql_18}/bin" \
                --set-default IGLOO_PYTHON "${tiktokPython}/bin/python3"
              wrapProgram "$out/bin/igloo-adduser" --prefix PATH : "${lib.getBin pkgs.postgresql_18}/bin"
            '';

            doCheck = false;

            meta = {
              description = "Local-first video archive server";
              homepage = "https://github.com/screwys/igloo";
              license = lib.licenses.gpl3Plus;
              mainProgram = "igloo";
              platforms = lib.platforms.linux;
            };
          };

          runtimeEnv = pkgs.buildEnv {
            name = "igloo-runtime";
            paths = [
              igloo
              pkgs.cacert
              (lib.getBin pkgs.ffmpeg-headless)
              galleryDl
              ytDlp
              tiktokPython
              (lib.getBin pkgs.postgresql_18)
            ];
            pathsToLink = [
              "/bin"
              "/etc"
              "/share"
            ];
          };

          containerEntrypoint = pkgs.writeShellScriptBin "igloo-entrypoint" (
            builtins.readFile ./scripts/container-entrypoint.sh
          );

          container = pkgs.dockerTools.buildLayeredImage {
            name = containerImageName;
            tag = "latest";
            maxLayers = 120;

            contents = [
              runtimeEnv
              containerEntrypoint
              (pkgs.dockerTools.fakeNss.override {
                extraPasswdLines = [
                  "igloo:x:10001:10001:Igloo:/tmp:/bin/sh"
                  "postgres:x:999:999:PostgreSQL:/var/empty:/bin/sh"
                ];
                extraGroupLines = [
                  "igloo:x:10001:"
                  "postgres:x:999:"
                ];
              })
            ];

            extraCommands = ''
              mkdir -p app usr/local/bin igloo/data igloo/config tmp
              chmod 1777 tmp
              : > igloo/data/.igloo-state-root

              ln -s ${igloo}/share/igloo/static app/static
              ln -s ${igloo}/share/igloo/locales app/locales
              ln -s ${igloo}/bin/igloo usr/local/bin/igloo
              ln -s ${igloo}/bin/igloo-adduser usr/local/bin/igloo-adduser
            '';

            fakeRootCommands = ''
              chown -R 10001:10001 igloo
            '';

            config = {
              Entrypoint = [ "${containerEntrypoint}/bin/igloo-entrypoint" ];
              Cmd = [ "/usr/local/bin/igloo" ];
              Env = [
                "PATH=/usr/local/bin:${runtimeEnv}/bin:/bin"
                "SSL_CERT_FILE=${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt"
                "REQUESTS_CA_BUNDLE=${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt"
                "LANG=C.UTF-8"
                "HOME=/tmp"
                "IGLOO_DATA_DIR=/igloo/data"
                "IGLOO_CONFIG_DIR=/igloo/config"
                "IGLOO_REPO_DIR=/app"
                "IGLOO_PORT=5001"
                "IGLOO_ENABLED_PLATFORMS=all"
                "IGLOO_PYTHON=${tiktokPython}/bin/python3"
              ];
              ExposedPorts = {
                "5001/tcp" = { };
              };
              Volumes = {
                "/igloo" = { };
              };
              User = "10001:10001";
              WorkingDir = "/app";
            };
          };
        in
        {
          default = igloo;
          inherit container igloo;
          postgresql = lib.getBin pkgs.postgresql_18;
          gallery-dl = galleryDl;
          yt-dlp = ytDlp;
          python-runtime = tiktokPython;
        }
      );

      apps = forAllSystems (
        system:
        let
          pkg = self.packages.${system}.igloo;
        in
        {
          default = self.apps.${system}.igloo;
          igloo = {
            type = "app";
            program = "${pkg}/bin/igloo";
          };
        }
      );

      checks = forAllSystems (system: {
        default = self.packages.${system}.igloo;
        container = self.packages.${system}.container;
      });

      devShells = forAllSystems (
        system:
        let
          pkgs = import nixpkgs { inherit system; };
          go = goFor system pkgs;
        in
        {
          default = pkgs.mkShell {
            packages = [
              go
              pkgs.just
              (pkgs.lib.getBin pkgs.ffmpeg-headless)
              self.packages.${system}.gallery-dl
              self.packages.${system}.yt-dlp
              self.packages.${system}.python-runtime
              (pkgs.lib.getBin pkgs.postgresql_18)
            ];
            IGLOO_PYTHON = "${self.packages.${system}.python-runtime}/bin/python3";
          };
        }
      );
    };
}
