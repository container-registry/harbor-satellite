# Parsec Go Client End to End  (Continuous Integration) tests

Currently only the CI test with all providers enabled is working at all.  To run it, you will need docker.

```bash
./scripts/ci-all.sh
```

The ci-*.sh scripts in the scripts folder all build docker images defined in the provider_cfg folder, which has subfolders for each of the provider configurations.

The docker containers run the ci.sh script in the scripts folder.  