{
  description = "Harbor Satellite peer-copy MicroVM testbed";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    microvm.url = "github:microvm-nix/microvm.nix";
    microvm.inputs.nixpkgs.follows = "nixpkgs";
  };

  outputs = { self, nixpkgs, microvm }:
    let
      system = "x86_64-linux";
      repoRoot = builtins.getEnv "SATELLITE_ROOT";
    in {
      nixosConfigurations.smoke = nixpkgs.lib.nixosSystem {
        inherit system;
        modules = [
          microvm.nixosModules.microvm
          ./modules/common.nix
          {
            networking.hostName = "smoke";
            microvm.vcpu = 1;
            microvm.mem = 512;
            microvm.interfaces = [{
              type = "user";
              id = "usr0";
              mac = "02:00:00:00:00:01";
            }];
          }
        ];
      };

      nixosConfigurations.sat-a = nixpkgs.lib.nixosSystem {
        inherit system;
        specialArgs = { inherit repoRoot; };
        modules = [
          microvm.nixosModules.microvm
          ./modules/common.nix
          ./modules/sat-a.nix
        ];
      };

      nixosConfigurations.sat-b = nixpkgs.lib.nixosSystem {
        inherit system;
        specialArgs = { inherit repoRoot; };
        modules = [
          microvm.nixosModules.microvm
          ./modules/common.nix
          ./modules/sat-b.nix
        ];
      };

      packages.${system} = {
        smoke = self.nixosConfigurations.smoke.config.microvm.declaredRunner;
        sat-a =
          if repoRoot == "" then
            throw ''
              SATELLITE_ROOT is empty. From the repository root:

                export SATELLITE_ROOT="$PWD"
                cd test/microvm
                nix run --impure .#sat-a
            ''
          else
            self.nixosConfigurations.sat-a.config.microvm.declaredRunner;
        sat-b =
          if repoRoot == "" then
            throw ''
              SATELLITE_ROOT is empty. From the repository root:

                export SATELLITE_ROOT="$PWD"
                cd test/microvm
                nix run --impure .#sat-b
            ''
          else
            self.nixosConfigurations.sat-b.config.microvm.declaredRunner;
      };
    };
}
