{ pkgs ? import <nixpkgs> { overlays = [ (import ./overlay.nix) ]; }
, supportedSystems ? [ "x86_64-linux" ]
}:

let
  makeNetboot = config:
    let
      config_evaled = import "${pkgs.path}/nixos/lib/eval-config.nix" config;
      build = config_evaled.config.system.build;
    in
      pkgs.symlinkJoin {
        name="netboot";
        paths=[
          build.netbootRamdisk
          build.kernel
          build.netbootIpxeScript
          build.ceranaGrub2Config
        ];
      };
in rec {

  ipxe = pkgs.ipxe;
  cerana = pkgs.cerana;
  cerana-scripts = pkgs.cerana-scripts;

  minimal_media = makeNetboot {
    system = "x86_64-linux";
    modules = import ./modules/module-list.nix ++ [
      { nixpkgs = { overlays = [ (import ./overlay.nix) ]; }; }
      "${pkgs.path}/nixos/modules/profiles/minimal.nix"
      ./modules/cerana/cerana.nix
      ./modules/profiles/cerana-hardware.nix
      ./modules/profiles/ceranaos.nix
    ];
  };

  minimal_iso = pkgs.stdenv.mkDerivation {
  system = "x86_64-linux";
  name = "cerana-iso";
  src = minimal_media;
  installPhase =
    ''
      ${pkgs.grub2}/bin/grub-mkrescue \
        -o $out \
        -V CERANA \
        --xorriso=${pkgs.xorriso}/bin/xorriso \
        -- \
        -follow on \
        -pathspecs on \
        -add boot/grub/grub.cfg=${minimal_media}/grub.cfg \
        bzImage=${minimal_media}/bzImage \
        initrd=${minimal_media}/initrd \
        ipxe.lkrn=${pkgs.ipxe}/ipxe.lkrn
    '';
  };

}