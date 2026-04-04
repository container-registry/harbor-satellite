# **PARSEC Integration & Zero-Trust Bootstrapping Flow**

(I used AI to make it look good but i am clear about all the steps that i listed).

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

* **Check for PARSEC:** The Satellite attempts to connect to the local PARSEC daemon via its API socket [Ping()](https://parallaxsecond.github.io/parsec-book/parsec_client/operations/ping.html).  
* **Hardware Mode (Primary):** If PARSEC is present and responsive, initialize the `ParsecKeyProvider`. All cryptographic operations will be routed to the hardware[generateKey](https://parallaxsecond.github.io/parsec-book/parsec_client/operations/psa_generate_key.html).  
* **Software Mode (Fallback):** If PARSEC is absent (e.g., local development or unsupported VMs), the system checks for a local file-based configuration. If permitted, it initializes the `FileKeyProvider`, degrading gracefully to software-based key storage.


### **Step 2: Key Management & Identity Generation**

Once the provider is established, the Satellite checks for its cryptographic identity.

* **Key Check:** The `KeyProvider` checks if a Private/Public key pair already exists (either in the hardware or on disk).  
* **Load Keys:** If keys are present, it loads the key references into memory via the PARSEC API (the private key material never leaves the hardware)[importKey](https://parallaxsecond.github.io/parsec-book/parsec_client/operations/psa_import_key.html).  
* **Generate Keys:** If no keys are found (first boot), the Satellite instructs PARSEC to generate a new, non-exportable asymmetric key pair.

### **Step 3: Attestation & Network Routing**

The Satellite must prepare to prove its identity to Harbor Ground Control, which depends on network availability.

* **Generate Quote:** The Satellite uses PARSEC to generate a Certificate Signing Request (CSR) and an "Attestation Quote" (cryptographic proof that the keys reside in secure hardware).  
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

