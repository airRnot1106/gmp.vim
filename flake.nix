{
  description = "Markdown preview for Vim and Neovim on goda.vim";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    goda-vim = {
      url = "github:Shougo/goda.vim";
      flake = false;
    };
  };

  outputs =
    {
      self,
      nixpkgs,
      goda-vim,
    }:
    let
      systems = [
        "aarch64-darwin"
        "x86_64-darwin"
        "aarch64-linux"
        "x86_64-linux"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
      version = "0.1.0";

      mkPackages = pkgs: rec {
        client = pkgs.buildNpmPackage {
          pname = "gmp-client";
          inherit version;
          src = ./client;
          npmDepsHash = "sha256-oSytPz31fwkph+XdxHmFMUqjHGoq5137yWoHFhy+Q+Q=";
          installPhase = ''
            runHook preInstall
            cp -r dist $out
            runHook postInstall
          '';
        };

        gmp = pkgs.buildGo126Module {
          pname = "gmp";
          inherit version;
          src = ./.;
          vendorHash = "sha256-YaflM80d4knBldJdqNpmYF8k9vOAW4JtSG6WIVs3hY8=";
          subPackages = [ "." ];
          doCheck = false;
          preBuild = ''
            rm -rf client/dist
            cp -r ${client} client/dist
            chmod -R u+w client/dist
          '';
          postInstall = ''
            mv $out/bin/gmp.vim $out/bin/gmp
          '';
          meta.mainProgram = "gmp";
        };

        goda-vim-plugin = pkgs.vimUtils.buildVimPlugin {
          pname = "goda.vim";
          version = "0-${goda-vim.shortRev}";
          src = goda-vim;
        };

        vim-plugin = pkgs.vimUtils.buildVimPlugin {
          pname = "gmp.vim";
          inherit version;
          src = pkgs.lib.fileset.toSource {
            root = ./.;
            fileset = pkgs.lib.fileset.unions [
              ./LICENSE
              ./autoload
              ./doc
              ./ftplugin
            ];
          };
          dependencies = [ goda-vim-plugin ];
          preInstall = ''
            mkdir bin
            cp ${gmp}/bin/gmp bin/gmp
          '';
        };

        default = vim-plugin;
      };

      mkDemo =
        pkgs: editor:
        let
          built = mkPackages pkgs;
          init = pkgs.writeText "gmp-demo.vim" ''
            set runtimepath^=${built.goda-vim-plugin},${built.vim-plugin}
            autocmd VimEnter * PreviewMarkdown
          '';
          script = pkgs.writeShellApplication {
            name = "gmp-demo";
            text = ''
              dir=$(mktemp -d)
              trap 'rm -rf "$dir"' EXIT
              cp ${./demo}/* "$dir"
              chmod -R u+w "$dir"
              for i in $(seq 1 50); do
                printf '\n## Section %s\n\nScroll target %s.\n' "$i" "$i" >> "$dir/sample.md"
              done
              ${pkgs.lib.getExe editor} --clean --cmd 'source ${init}' "$@" "$dir/sample.md"
            '';
          };
        in
        {
          type = "app";
          program = pkgs.lib.getExe script;
          meta.description = "Open a sample Markdown file in ${editor.pname} with only gmp.vim and goda.vim";
        };
    in
    {
      apps = forAllSystems (pkgs: rec {
        default = neovim;
        neovim = mkDemo pkgs pkgs.neovim;
        vim = mkDemo pkgs pkgs.vim;
      });

      packages = forAllSystems (
        pkgs:
        let
          built = mkPackages pkgs;
        in
        {
          inherit (built)
            client
            gmp
            vim-plugin
            default
            ;
          goda-vim = built.goda-vim-plugin;
        }
      );

      checks = forAllSystems (
        pkgs:
        let
          built = mkPackages pkgs;
          goTest =
            name: command:
            built.gmp.overrideAttrs {
              pname = "gmp-${name}";
              doCheck = true;
              __darwinAllowLocalNetworking = true;
              checkPhase = ''
                runHook preCheck
                ${command}
                runHook postCheck
              '';
              installPhase = "touch $out";
              postInstall = "";
            };
        in
        {
          unit = goTest "unit" "go test ./internal/...";
          e2e = goTest "e2e" ''
            export HOME=$TMPDIR
            export PATH=${
              pkgs.lib.makeBinPath [
                pkgs.vim
                pkgs.neovim
              ]
            }:$PATH
            export GMP_E2E_GODA=${goda-vim}
            export GMP_E2E_STRICT=1
            go test -v -count=1 ./e2e
          '';
          goda-pin = pkgs.runCommand "gmp-goda-pin" { } ''
            grep -E 'github.com/Shougo/goda.vim v[^ ]*-${builtins.substring 0 12 goda-vim.rev}( |$)' ${./go.mod}
            touch $out
          '';
        }
      );

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = [
            pkgs.go_1_26
            pkgs.nodejs
            pkgs.vim
            pkgs.neovim
          ];
        };
      });
    };
}
