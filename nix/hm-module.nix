# home-manager module: programs.wimy.
#
# macOS: the activation copies Wimy.app out of the store to installDir
# (a stable, writable path — wimy's in-place restart re-executes the
# path it runs from, and the signature, not the path, carries the
# Accessibility permission), links wimyctl into ~/.local/bin, and then
# restarts a running wimy in place (state kept; the new build is checked
# first) or starts it. Login start is wimy's own start-at-login.
# Linux: installs the package; start it from river (`river -c wimy`).
self:
{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.programs.wimy;
  inherit (pkgs.stdenv.hostPlatform) isDarwin;
  app = "${cfg.installDir}/Wimy.app";
in
{
  options.programs.wimy = {
    enable = lib.mkEnableOption "wimy, a wmii-style tiling window manager";
    package = lib.mkOption {
      type = lib.types.package;
      default = self.packages.${pkgs.stdenv.hostPlatform.system}.wimy;
      defaultText = lib.literalExpression "wimy.packages.\${system}.wimy";
      description = "The wimy package (on macOS: the signed release app).";
    };
    settings = lib.mkOption {
      type = with lib.types; nullOr (either path lines);
      default = null;
      example = "bar-gap 37";
      description = ''
        ~/.config/wimy/config.kdl, as a path or KDL text. null leaves
        that file alone (e.g. when it is linked some other way).
      '';
    };
    installDir = lib.mkOption {
      type = lib.types.str;
      default = "${config.home.homeDirectory}/Applications";
      description = "Where Wimy.app is installed on macOS.";
    };
  };

  config = lib.mkIf cfg.enable (
    lib.mkMerge [
      (lib.mkIf (!isDarwin) { home.packages = [ cfg.package ]; })
      (lib.mkIf (cfg.settings != null) {
        xdg.configFile."wimy/config.kdl" =
          if builtins.isPath cfg.settings then { source = cfg.settings; } else { text = cfg.settings; };
      })
      (lib.mkIf isDarwin {
        home.activation.wimy = lib.hm.dag.entryAfter [ "writeBoundary" "linkGeneration" ] ''
          run mkdir -p ${lib.escapeShellArg cfg.installDir} "$HOME/.local/bin"
          # store files are read-only: make the copy writable so the next
          # update can replace it (permissions aren't part of the signature)
          run ${pkgs.rsync}/bin/rsync -a --delete --chmod=u+w \
            ${cfg.package}/Applications/Wimy.app/ ${lib.escapeShellArg app}/
          run ln -sf ${lib.escapeShellArg "${app}/Contents/MacOS/wimyctl"} "$HOME/.local/bin/wimyctl"
          if ${lib.escapeShellArg "${app}/Contents/MacOS/wimyctl"} version >/dev/null 2>&1; then
            run ${lib.escapeShellArg "${app}/Contents/MacOS/wimyctl"} restart \
              || warnEcho "wimy: restart failed; the running wimy stays (see ~/Library/Logs/wimy.log)"
          else
            run /usr/bin/open ${lib.escapeShellArg app}
          fi
        '';
      })
    ]
  );
}
