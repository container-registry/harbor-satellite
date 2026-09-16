{ pkgs, ... }:
{
  networking.firewall.enable = false;
  users.users.root.password = "";
  services.getty.autologinUser = "root";
  environment.systemPackages = [ pkgs.iputils pkgs.curl pkgs.iproute2 ];

  microvm = {
    hypervisor = "qemu";
    shares = [{
      proto = "9p";
      tag = "ro-store";
      source = "/nix/store";
      mountPoint = "/nix/.ro-store";
    }];
  };
}
