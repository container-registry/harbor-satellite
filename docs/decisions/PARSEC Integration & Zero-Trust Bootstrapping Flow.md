# **PARSEC Integration & Zero-Trust Bootstrapping Flow**

## **Objective**

To implement hardware-backed device identity at the edge for Harbor Satellite [327](https://github.com/container-registry/harbor-satellite/issues/327). This flow ensures that a Satellite's cryptographic identity is strictly bound to its physical hardware (TPM/Secure Enclave) via the CNCF PARSEC API, preventing credential theft and configuration cloning.

## **Assumptions**

* **PARSEC Daemon:** We expect the PARSEC service (daemon) to be actively running on the underlying Edge OS.  
* **Ground Control Reachability:** Ground Control is deployed and capable of receiving HTTP/mTLS requests (either directly or via out-of-band admin uploads).  
* **Hardware Support:** The edge device has a secure element (TPM 2.0, ARM TrustZone, etc.) supported by PARSEC.

---

## **Execution Flow**

### **Step 1: Hardware Abstraction & Initialization**

When the Harbor Satellite service starts, it must determine its cryptographic backend.

Provider selection is driven by the operator flag `--parsec-enabled` (env `PARSEC_ENABLED`). The two paths are mutually exclusive and **never fall back into each other**, per ADR-0007:

* **Hardware Mode (`--parsec-enabled` set):** The Satellite attempts to connect to the local PARSEC daemon via its API socket [Ping()](https://parallaxsecond.github.io/parsec-book/parsec_client/operations/ping.html). If the daemon is unreachable or the `ParsecKeyProvider` fails to initialize, the Satellite **halts with an error** — there is no silent fallback. All cryptographic operations are routed to the hardware ([generateKey](https://parallaxsecond.github.io/parsec-book/parsec_client/operations/psa_generate_key.html)).
* **Software Mode (`--parsec-enabled` not set):** The Satellite initializes the software `crypto.Provider` (AES-GCM with device-bound key derivation). PARSEC is never contacted; the PARSEC socket is irrelevant. This is the default for VMs, development, and any deployment without secure-element hardware.


### **Step 2: Key Management & Identity Generation**

Once the provider is established, the Satellite checks for its cryptographic identity.

* **Key Check:** The `KeyProvider` calls [listKeys](https://parallaxsecond.github.io/parsec-book/parsec_client/operations/list_keys.html) on the PARSEC daemon and looks for a key with the well-known name (`satellite-identity-key`).
* **Existing Key:** If the named key is present, the Satellite uses it by reference. There is no "load" operation — keys in PARSEC are addressed by `key_name`; the private key material never leaves the secure element.
* **Generate Key:** If the named key is absent (first boot), the Satellite calls [psa_generate_key](https://parallaxsecond.github.io/parsec-book/parsec_client/operations/psa_generate_key.html) to provision a new non-exportable asymmetric key pair under that name. Note: `psa_import_key` is for importing externally-supplied raw key material into PARSEC and is **not** used to load existing in-hardware keys.

### **Step 3: Attestation & Network Routing**

The Satellite must prepare to prove its identity to Harbor Ground Control, which depends on network availability.

* **Build CSR & Quote:** The Satellite builds the ASN.1 DER Certificate Signing Request (CSR) in application code and uses PARSEC's [psa_sign_hash](https://parallaxsecond.github.io/parsec-book/parsec_client/operations/psa_sign_hash.html) to sign the `CertificationRequestInfo` block with the hardware-resident identity key. PARSEC itself does not synthesize CSRs — it only signs the digest. In parallel, the Satellite obtains an "Attestation Quote" (cryptographic proof that the identity key resides in secure hardware) from the platform attestation backend (e.g. TPM2 quote via SPIRE's `tpm_devid` plugin once ADR-0005 Phase 2 lands).
* **Check Connectivity:** The Satellite checks if it has sufficient internet connectivity to reach Ground Control.  
  * **Path A (Connected):** If the network is available, it POSTs the CSR and Attestation Quote directly to Ground Control's `/register` API endpoint.  
  * **Path B (Air-Gapped):** If there is no network, the Satellite exports the CSR and Quote to a local file/USB. An administrator must manually upload this file to the Ground Control web UI, generate a sealed response bundle, and carry it back to the Satellite via USB.

### **Step 4: Ground Control Validation & SPIFFE Issuance**

Ground Control acts as the gatekeeper, validating the incoming registration request.

* **Verify Hardware Proof:** Ground Control inspects the Attestation Quote. It mathematically verifies that the request came from a legitimate, untampered hardware chip (verifying against trusted manufacturer certificates).  
* **Rejection:** If the quote is invalid, spoofed, or missing, the registration is rejected.  
* **Issuance:** If valid, Ground Control registers the device and issues a SPIFFE SVID (an X.509 certificate) bound specifically to that physical Satellite.

### **Step 5: Operational mTLS & Config Unsealing**

With the SVID in hand, the Satellite can securely boot its core services.

* **Establish mTLS:** The Satellite uses its new X.509 SVID to establish a Mutual TLS (mTLS) tunnel with Ground Control.  
* **Fetch Credentials:** Over this secure tunnel, the Satellite fetches its short-lived Robot Account credentials to access Harbor registries.  
* **Unseal Config:** The Satellite passes its encrypted local `config.json` payload to PARSEC. PARSEC decrypts it using the hardware-bound key, allowing the Satellite to read its settings and become fully operational.

### **Step 6: Credential Rotation Lifecycle**

To maintain zero-trust principles, the Satellite's working credentials are intentionally short-lived.

* **Heartbeat:** Before the Robot credentials expire, the Satellite sends a heartbeat ping over the mTLS tunnel.  
* **Refresh:** Because the mTLS tunnel guarantees the hardware identity, Ground Control trusts the request and issues a fresh set of Robot credentials for the next cycle.

---

