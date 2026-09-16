{ repoRoot, ... }:
{
  networking.hostName = "sat-b";

  microvm = {
    vcpu = 2;
    # 2048 hangs QEMU at KASLR (microvm.nix#171). 2049 is the workaround.
    mem = 2049;
    interfaces = [{
      type = "user";
      id = "usr0";
      mac = "02:00:00:00:00:0b";
    }];
    forwardPorts = [
      { from = "host"; host.port = 5001; guest.port = 5000; }
    ];
    shares = [
      {
        proto = "9p";
        tag = "sat-bin";
        source = "${repoRoot}/bin";
        mountPoint = "/mnt/satellite-bin";
      }
      {
        proto = "9p";
        tag = "sat-secrets";
        source = "${repoRoot}/test/microvm/secrets";
        mountPoint = "/mnt/satellite-secrets";
      }
    ];
    volumes = [{
      image = "sat-b-data.img";
      mountPoint = "/var/lib/satellite";
      size = 4096;
    }];
  };

  systemd.services.harbor-satellite = {
    description = "Harbor Satellite (host-built binary)";
    wantedBy = [ "multi-user.target" ];
    after = [ "network.target" ];
    serviceConfig = {
      Type = "simple";
      WorkingDirectory = "/var/lib/satellite";
      EnvironmentFile = "/mnt/satellite-secrets/sat-b.env";
      Environment = [
        "CONFIG_DIR=/var/lib/satellite"
        "GROUND_CONTROL_URL=http://10.0.2.2:7080"
        "REGISTRY_LISTEN=:5000"
        "USE_UNSECURE=true"
        # A's replica proxy via QEMU user-net gateway (host :5000 → sat-a :5000).
        "PEER_URLS=http://10.0.2.2:5000"
      ];
      ExecStart = "/mnt/satellite-bin/satellite";
      Restart = "on-failure";
      RestartSec = "5s";
    };
  };
}
