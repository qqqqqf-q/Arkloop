{
  description = "Arkloop desktop application and development environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
  };

  outputs =
    { self, nixpkgs, ... }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      linuxSystems = [
        "x86_64-linux"
        "aarch64-linux"
      ];

      forAllSystems =
        f:
        nixpkgs.lib.genAttrs systems (
          system:
          f (
            import nixpkgs {
              inherit system;
            }
          )
        );

      forLinuxSystems =
        f:
        nixpkgs.lib.genAttrs linuxSystems (
          system:
          f (
            import nixpkgs {
              inherit system;
            }
          )
        );

      desktopVersion = (builtins.fromJSON (builtins.readFile ./src/apps/desktop/package.json)).version;

      sourceFilter =
        lib: path: type:
        let
          root = toString ./.;
          rel = lib.removePrefix "${root}/" (toString path);
          excluded = [
            ".cache"
            ".data"
            ".env"
            ".envrc"
            ".git"
            ".gstack"
            ".mypy_cache"
            ".pytest_cache"
            ".ruff_cache"
            ".vscode"
            "AGENTS.md"
            "CLAUDE.md"
            "data"
            "mcp.config.json"
            "node_modules"
            "result"
            "result-arkloop-desktop"
            "config/openviking/ov.conf"
            "src/apps/desktop/cli-bin"
            "src/apps/desktop/dist"
            "src/apps/desktop/release"
            "src/apps/desktop/sidecar-bin"
            "src/apps/web/dist"
          ];
        in
        !(lib.any (prefix: rel == prefix || lib.hasPrefix "${prefix}/" rel) excluded)
        && !(lib.hasPrefix ".env." rel);

      cleanSource =
        pkgs:
        pkgs.lib.cleanSourceWith {
          src = ./.;
          filter = sourceFilter pkgs.lib;
        };

      electronArchFor = pkgs: if pkgs.stdenv.hostPlatform.isAarch64 then "arm64" else "x64";

      buildGo126 = pkgs: pkgs.buildGoModule.override { go = pkgs.go_1_26; };

      goBinaries =
        pkgs:
        (buildGo126 pkgs) {
          pname = "arkloop-desktop-go-binaries";
          version = desktopVersion;
          src = cleanSource pkgs;
          modRoot = "./";
          subPackages = [
            "src/services/activity-record/cmd/activity-record"
            "src/services/cli/cmd/ark"
            "src/services/desktop/cmd/desktop"
          ];
          tags = [ "desktop" ];
          ldflags = [
            "-s"
            "-w"
            "-X main.version=v${desktopVersion}"
          ];
          vendorHash = "sha256-HSP4IMyafSLyMI7bt7VKqOU2EHDkB/PNdELhoSSfjUI=";
          env.CGO_ENABLED = "0";
          doCheck = false;
          modBuildPhase = ''
            runHook preBuild
            go work vendor
            mkdir -p vendor
            runHook postBuild
          '';
        };

      arkloopDesktop =
        pkgs:
        let
          lib = pkgs.lib;
          pnpm = pkgs.pnpm_10_29_2;
          electronArch = electronArchFor pkgs;
          goBin = goBinaries pkgs;
          gpuLibraryPath = lib.makeLibraryPath [
            pkgs.libglvnd
            pkgs.libva
            pkgs.mesa
            pkgs.vulkan-loader
          ];
          runtimePath = lib.makeBinPath [
            pkgs.bash
            pkgs.coreutils
            pkgs.docker-client
            pkgs.docker-compose
            pkgs.git
            pkgs.openssl
            pkgs.xdg-utils
          ];
        in
        pkgs.stdenv.mkDerivation (finalAttrs: {
          pname = "arkloop-desktop";
          version = desktopVersion;
          src = cleanSource pkgs;

          pnpmDeps = pkgs.fetchPnpmDeps {
            inherit (finalAttrs) pname version src;
            pnpm = pnpm;
            fetcherVersion = 4;
            hash = "sha256-ycM0fYo1O3exXS3jMgarfXei4mMWEFx7nhw1X6UOR5Y=";
          };

          nativeBuildInputs = [
            pkgs.go_1_26
            pkgs.jq
            pkgs.makeWrapper
            pkgs.nodejs_24
            pkgs.pnpmConfigHook
            pkgs.wrapGAppsHook3
            pnpm
          ];

          buildInputs = [
            pkgs.glib
            pkgs.gsettings-desktop-schemas
            pkgs.gtk3
            pkgs.gtk4
          ];

          dontWrapGApps = true;

          env = {
            ELECTRON_SKIP_BINARY_DOWNLOAD = "1";
            GOTOOLCHAIN = "local";
          };

          postPatch = ''
            substituteInPlace package.json src/apps/desktop/package.json \
              --replace-fail '"packageManager": "pnpm@10.29.3"' '"packageManager": "pnpm@${pnpm.version}"'
          '';

          preBuild = ''
            export HOME="$TMPDIR/home"
            export XDG_CACHE_HOME="$TMPDIR/xdg-cache"
            export ELECTRON_BUILDER_CACHE="$TMPDIR/electron-builder-cache"
            export npm_config_fund=false
            export npm_config_update_notifier=false
            export pnpm_config_manage_package_manager_versions=false
            export ARKLOOP_RELEASE_VERSION="v${desktopVersion}"

            mkdir -p "$HOME" "$XDG_CACHE_HOME" "$ELECTRON_BUILDER_CACHE" "$TMPDIR/nix-bin"
            cat > "$TMPDIR/nix-bin/pnpm" <<'EOF'
            #!${pkgs.runtimeShell}
            exec ${pnpm}/bin/pnpm --config.manage-package-manager-versions=false "$@"
            EOF
            chmod +x "$TMPDIR/nix-bin/pnpm"
            export PATH="$TMPDIR/nix-bin:$PATH"

            mkdir -p src/apps/desktop/sidecar-bin src/apps/desktop/cli-bin
            cp ${goBin}/bin/desktop src/apps/desktop/sidecar-bin/desktop-linux-${electronArch}
            cp ${goBin}/bin/ark src/apps/desktop/cli-bin/ark-linux-${electronArch}
          '';

          buildPhase = ''
            runHook preBuild

            pnpm --config.manage-package-manager-versions=false --filter @arkloop/desktop run build:web
            pnpm --config.manage-package-manager-versions=false --filter @arkloop/desktop run build:electron
            pnpm --config.manage-package-manager-versions=false --dir src/apps/desktop exec electron-builder \
              --linux dir \
              -c.electronDist=${pkgs.electron_41.dist} \
              -c.electronVersion=${pkgs.electron_41.version} \
              -c.extraMetadata.version=${desktopVersion}

            runHook postBuild
          '';

          installPhase = ''
            runHook preInstall

            release_dir="src/apps/desktop/release/linux-unpacked"
            if [ ! -d "$release_dir" ]; then
              echo "missing electron-builder output: $release_dir" >&2
              exit 1
            fi

            if [ ! -d "$release_dir/resources" ]; then
              echo "missing electron-builder resources: $release_dir/resources" >&2
              exit 1
            fi
            if [ ! -f "$release_dir/resources/app.asar" ]; then
              echo "missing electron app archive: $release_dir/resources/app.asar" >&2
              exit 1
            fi

            resources_dir="$out/share/arkloop/resources"
            mkdir -p "$resources_dir" "$out/bin" "$out/share/applications" "$out/share/icons/hicolor/512x512/apps"
            cp -R "$release_dir/resources/." "$resources_dir/"

            mkdir -p "$resources_dir/activity-record/bin"
            cp ${goBin}/bin/activity-record "$resources_dir/activity-record/bin/activity-record"

            mkdir -p "$out/libexec/arkloop"
            cat > "$out/libexec/arkloop/arkloop" <<EOF
            #!${pkgs.runtimeShell}
            gpu_flags=(--ignore-gpu-blocklist --enable-gpu-rasterization --enable-zero-copy --disable-gpu-sandbox --ozone-platform=x11)
            if [ "\''${ARKLOOP_DISABLE_GPU:-}" = "1" ]; then
              gpu_flags=(--disable-gpu --disable-gpu-compositing)
            fi
            exec ${pkgs.electron_41}/bin/electron "\''${gpu_flags[@]}" "$resources_dir/app.asar" "\''${@}"
            EOF
            chmod +x "$out/libexec/arkloop/arkloop"

            gappsWrapperArgsHook
            makeWrapper "$out/libexec/arkloop/arkloop" "$out/bin/arkloop" \
              "''${gappsWrapperArgs[@]}" \
              --set ELECTRON_FORCE_IS_PACKAGED 1 \
              --set ARKLOOP_NIX_PACKAGE 1 \
              --set ARKLOOP_RESOURCES_PATH "$resources_dir" \
              --set ARKLOOP_DISABLE_APP_UPDATER 1 \
              --set ARKLOOP_ACTIVITY_RECORD_BIN "$resources_dir/activity-record/bin/activity-record" \
              --set CHROME_DEVEL_SANDBOX ${pkgs.electron_41}/libexec/electron/chrome-sandbox \
              --set LIBVA_DRIVERS_PATH /run/opengl-driver/lib/dri \
              --prefix LD_LIBRARY_PATH : ${lib.escapeShellArg "${gpuLibraryPath}:/run/opengl-driver/lib"} \
              --prefix PATH : ${lib.escapeShellArg runtimePath}

            makeWrapper "$resources_dir/cli/ark" "$out/bin/ark" \
              --prefix PATH : ${lib.escapeShellArg runtimePath}

            install -Dm644 src/apps/desktop/resources/icon.png "$out/share/icons/hicolor/512x512/apps/arkloop.png"
            cat > "$out/share/applications/arkloop.desktop" <<EOF
            [Desktop Entry]
            Name=Arkloop
            Comment=Desktop app for building conversational AI agents
            Exec=$out/bin/arkloop %U
            Terminal=false
            Type=Application
            Icon=arkloop
            Categories=Development;
            StartupWMClass=Arkloop
            EOF

            runHook postInstall
          '';

          meta = {
            description = "Desktop app for building conversational AI agents";
            homepage = "https://github.com/qqqqqf-q/Arkloop";
            license = lib.licenses.asl20;
            mainProgram = "arkloop";
            platforms = linuxSystems;
          };
        });
    in
    {
      packages = forLinuxSystems (pkgs: rec {
        arkloop-desktop = arkloopDesktop pkgs;
        default = arkloop-desktop;
      });

      apps = forLinuxSystems (
        pkgs:
        let
          system = pkgs.stdenv.hostPlatform.system;
        in
        rec {
          arkloop-desktop = {
            type = "app";
            program = "${self.packages.${system}.arkloop-desktop}/bin/arkloop";
            meta.description = "Run Arkloop Desktop";
          };
          default = arkloop-desktop;
        }
      );

      devShells = forAllSystems (
        pkgs:
        let
          pnpm = pkgs.writeShellApplication {
            name = "pnpm";
            runtimeInputs = [ pkgs.corepack ];
            text = ''
              exec corepack pnpm "$@"
            '';
          };
        in
        {
          default = pkgs.mkShell {
            packages = with pkgs; [
              act
              bash
              corepack
              curl
              docker-client
              docker-compose
              git
              gnumake
              go_1_26
              jq
              nodejs_24
              openssl
              pkg-config
              pnpm
              postgresql_16
              redis
            ];

            GOTOOLCHAIN = "local";

            shellHook = ''
              export COREPACK_HOME="$PWD/.cache/corepack"
              mkdir -p "$COREPACK_HOME"

              echo "Arkloop dev shell"
              echo "  Go:   $(go version | awk '{print $3}')"
              echo "  Node: $(node --version)"
              echo "  pnpm: managed by Corepack from package.json"
              echo ""
              echo "Common commands:"
              echo "  pnpm install --frozen-lockfile"
              echo "  bin/ci-local quick"
              echo "  docker compose up -d postgres redis"
            '';
          };
        }
      );

      checks = forAllSystems (
        pkgs:
        let
          pnpm = pkgs.writeShellApplication {
            name = "pnpm";
            runtimeInputs = [ pkgs.corepack ];
            text = ''
              exec corepack pnpm "$@"
            '';
          };
        in
        {
          dev-tools =
            pkgs.runCommand "arkloop-dev-tools"
              {
                nativeBuildInputs = [
                  pkgs.corepack
                  pkgs.docker-client
                  pkgs.docker-compose
                  pkgs.go_1_26
                  pkgs.nodejs_24
                  pnpm
                ];
              }
              ''
                go version
                node --version
                corepack --version
                docker --version
                docker compose version
                touch "$out"
              '';
        }
      );

      formatter = forAllSystems (pkgs: pkgs.nixfmt);
    };
}
