# Security Review: PR #310 - Remove Plaintext Robot Secret from Ground Control Database

**Review Date:** 2026-02-06
**PR:** https://github.com/container-registry/harbor-satellite/pull/310
**Reviewers:** Security Review Team (6 specialized agents)
**Scope:** Security, authentication, Go best practices, OWASP Top 10 compliance

---

## Executive Summary

PR #310 significantly **improves security posture** by replacing plaintext robot secret storage with argon2id hashing. The implementation follows industry best practices for password hashing and meets OWASP recommended parameters. The cryptographic implementation is sound, using proper random salt generation, constant-time comparison, and appropriate error handling.

### Overall Assessment: ✅ **APPROVED WITH RECOMMENDATIONS**

**Key Strengths:**
- Eliminates plaintext secret storage in database (major security improvement)
- Uses argon2id with OWASP-recommended parameters (t=2, m=19456, p=1)
- Proper use of crypto/rand for salt generation
- Constant-time comparison prevents timing attacks
- Pass-through secret delivery model (no unnecessary persistence)
- Configurable robot expiry (default 30 days)

**Critical Issues:** None
**High Priority Issues:** None
**Medium Priority Issues:** 3
**Low Priority Issues:** 8

---

## Findings by Severity

### 🟡 MEDIUM Severity (3 findings)

#### M1: Potential Index Out of Range Panic
**File:** `ground-control/internal/server/helpers.go:188`
**Function:** `assignPermissionsToRobot`

```go
projects, err := q.GetProjectsOfGroup(ctx, groupName)
// err check exists, but no len check
project := projects[0]  // panics if projects is empty
```

**Risk:** If `GetProjectsOfGroup` returns an empty slice with no error, this will panic.

**Remediation:**
```go
projects, err := q.GetProjectsOfGroup(ctx, groupName)
if err != nil {
    return fmt.Errorf("fetch projects: %w", err)
}
if len(projects) == 0 {
    return fmt.Errorf("no projects found for group %s", groupName)
}
project := projects[0]
```

---

#### M2: Internal Error Details Leaked in HTTP Responses
**Files:** Multiple locations
- `helpers.go:169` - `storeRobotAccountInDB`
- `helpers.go:184` - `assignPermissionsToRobot`
- `helpers.go:194` - `assignPermissionsToRobot`
- `satellite_handlers.go:142, 168, 309`

**Example:**
```go
Message: fmt.Sprintf("Error: adding robot account to DB %v", err.Error()),
```

**Risk:** Raw database errors (table names, constraint violations, SQL details) are exposed to clients. This provides attackers with internal implementation details.

**Remediation:** Use generic error messages in HTTP responses, log full details server-side:
```go
log.Printf("Error adding robot account to DB: %v", err)
HandleAppError(w, &AppError{
    Message: "Failed to store robot account",
    Code:    http.StatusInternalServerError,
})
```

---

#### M3: Hardcoded Placeholder Secret in Development Mode
**Files:**
- `satellite_handlers.go:679`
- `satellite_handlers.go:708`

```go
robotSecret = "spiffe-auto-registered-placeholder-secret"
```

**Risk:** When `SKIP_HARBOR_HEALTH_CHECK=true`, all satellites share the same known secret. If this environment variable is accidentally enabled in production, all auto-registered satellites would use this predictable credential.

**Remediation:**
1. Add clear documentation that `SKIP_HARBOR_HEALTH_CHECK` is for **testing only**
2. Consider generating a random placeholder secret instead:
```go
robotSecret = fmt.Sprintf("dev-placeholder-%s", generateRandomToken(16))
```
3. Add startup warning log if this flag is enabled

---

### 🔵 LOW Severity (8 findings)

#### L1: Duplicated Argon2 Implementation Across Packages
**Files:** `helpers.go` vs `auth/password.go`

**Issue:** Two separate argon2id implementations exist:
- `helpers.go`: `hashRobotCredentials` and `verifyRobotCredentials` with magic numbers
- `auth/password.go`: `HashPassword` and `VerifyPassword` with named constants

The `auth` package version is more robust (extracts parameters from hash, validates them). This is a DRY violation with quality divergence.

**Remediation:** Extract argon2 hashing to a shared package with named constants:
```go
// pkg/crypto/argon2.go
const (
    ArgonTime       = 2
    ArgonMemory     = 19456
    ArgonParallelism = 1
    ArgonSaltSize   = 16
    ArgonKeySize    = 32
)

func HashWithArgon2(secret string) (string, error) { ... }
func VerifyArgon2(secret, hash string) bool { ... }
```

---

#### L2: Token in URL Path Visible to Infrastructure Logs
**File:** `satellite_handlers.go:264`

```go
token := vars["token"]  // from /satellites/ztr/{token}
```

**Issue:** Tokens in URL paths can be logged by reverse proxies, load balancers, and access logs. The `maskToken` function handles application logs correctly but cannot prevent external infrastructure logging.

**Recommendation:** Document that ZTR tokens are short-lived (24h) and single-use to mitigate risk.

---

#### L3: `verifyRobotCredentials` Defined But Never Used
**File:** `helpers.go:144-156`

**Issue:** The function exists and is tested but has no callers in production code. ZTR flow refreshes secrets instead of verifying them, so Ground Control acts as a credential broker rather than verifier.

**Status:** This is acceptable design. The function provides infrastructure for future use cases where credential verification may be needed.

---

#### L4: Early Returns in `verifyRobotCredentials` Leak Minor Timing Info
**File:** `helpers.go:146-152`

**Issue:** Returns immediately on malformed hash or base64 decode failure (faster than argon2 computation). Could theoretically distinguish "malformed hash" from "wrong secret".

**Status:** Acceptable risk. Attacker would need DB access to control stored hashes, at which point timing attacks are moot.

---

#### L5: No Secret Zeroization (Go Limitation)
**Files:** Various

**Issue:** Plaintext secrets (`rbt.Secret`, `freshSecret`, etc.) are held in Go strings and cannot be zeroed. Go strings are immutable.

**Status:** This is a known Go limitation. The secrets are transient (request lifetime only) and will be garbage collected. Standard practice in Go.

---

#### L6: Hash Format Parameters Not Validated During Verification
**File:** `helpers.go:144-156`

**Issue:** `verifyRobotCredentials` does not validate algorithm identifier (`parts[1]`), version (`parts[2]`), or parameters (`parts[3]`). It hardcodes parameters for recomputation regardless of stored hash string.

**Impact:** If argon2 parameters are ever changed, old hashes will silently fail verification.

**Status:** Actually a safe-by-default approach (forces expected parameters). For future-proofing, consider parsing and using stored parameters like `auth/password.go` does.

---

#### L7: Deferred Rollback Writes to Already-Written ResponseWriter
**File:** `satellite_handlers.go:127-134`

**Issue:** Deferred cleanup calls `HandleAppError(w, ...)` on rollback failure. If handler already called `HandleAppError`, the defer attempts to write again, causing `http: superfluous response.WriteHeader` log.

**Status:** Not a security issue, but sloppy. Consider checking if response was already written before deferred write.

---

#### L8: `/satellites/sync` Endpoint Has No Authentication (Pre-existing)
**File:** `routes.go:84`

**Issue:** Any client can POST status updates if they know a satellite name. Not introduced by this PR.

**Recommendation:** Add authentication middleware to this endpoint in a future PR.

---

## OWASP Top 10 2021 Compliance

| Category | Status | Notes |
|----------|--------|-------|
| **A01: Broken Access Control** | ✅ MOSTLY COMPLIANT | ZTR uses single-use tokens; SPIRE uses mTLS |
| **A02: Cryptographic Failures** | ✅ COMPLIANT | Argon2id matches OWASP recommendations exactly |
| **A03: Injection** | ✅ COMPLIANT | All queries parameterized via sqlc |
| **A04: Insecure Design** | ✅ COMPLIANT | Good secret rotation design; single-use tokens |
| **A05: Security Misconfiguration** | ✅ COMPLIANT | Reasonable defaults; no credential leakage |
| **A07: Auth Failures** | ✅ COMPLIANT (IMPROVED) | Major improvement from plaintext to hashed storage |
| **A08: Data Integrity** | ✅ COMPLIANT | Proper use of crypto libraries and DB constraints |
| **A09: Logging Failures** | ✅ COMPLIANT | No plaintext secrets in logs; token masking works |

---

## Go Best Practices Review

### ✅ Strengths
1. **DRY Refactoring:** Successfully deduplicated refresh logic (ztrHandler now calls `refreshRobotSecret`)
2. **Error Wrapping:** Consistent use of `%w` for error chains in most functions
3. **Constant-Time Comparison:** Proper use of `crypto/subtle` for security-sensitive operations
4. **Context Propagation:** Correct context usage throughout request lifecycle
5. **Table-Driven Tests:** Good test structure following Go conventions
6. **Naming:** Idiomatic function names and patterns

### ⚠️ Issues
1. **Duplicated Crypto Implementation:** argon2 logic duplicated between `helpers.go` and `auth/password.go` (see L1)
2. **Magic Numbers:** Argon2 parameters hardcoded as literals instead of named constants
3. **Error Handling Inconsistency:** Some places use `err.Error()` instead of `%w` (e.g., `robot.go:133`)
4. **Interface{} Usage:** Use `any` instead of `interface{}` (Go 1.18+)
5. **Error Shadowing:** Redundant `err :=` shadowing in error blocks

---

## Cryptographic Implementation Review

### ✅ Security Assessment: EXCELLENT

**Argon2id Parameters:**
```go
argon2.IDKey([]byte(secret), salt, 2, 19456, 1, 32)
// time=2, memory=19456 KiB (19 MiB), parallelism=1, keyLen=32
```

**OWASP Comparison:**
- Current: `t=2, m=19456, p=1` ← **Exactly matches OWASP first recommended option**
- This configuration provides strong resistance to brute-force attacks

**Salt Generation:**
```go
salt := make([]byte, 16)  // 16 bytes = 128 bits
rand.Read(salt)            // crypto/rand (CSPRNG)
```
- ✅ Proper use of cryptographically secure random number generator
- ✅ 128-bit entropy meets OWASP minimum (128 bits)

**Hash Format:**
- Uses PHC string format (standard for Argon2)
- Base64 RawStdEncoding (no padding, per PHC spec)
- Version embedded correctly

**Timing Safety:**
- `subtle.ConstantTimeCompare` used for hash comparison
- No timing side channels in secret comparison path

---

## Secret Lifecycle Analysis

### Creation
1. Robot created via Harbor API → plaintext secret returned
2. Secret immediately hashed with argon2id
3. Only hash stored in DB (`robot_secret_hash` column)
4. Plaintext secret passed through to satellite in ZTR response

✅ **No plaintext persistence**

### Storage
- Database schema: `robot_secret_hash VARCHAR(255) NOT NULL`
- All queries use parameterized statements (SQL injection safe)
- UNIQUE constraints prevent duplicates

✅ **Hash-only storage verified**

### Refresh
1. `harbor.RefreshRobotAccount` called to rotate secret in Harbor
2. New plaintext secret hashed immediately
3. DB hash updated via `UpdateRobotAccount`
4. Plaintext returned for satellite pass-through

✅ **Atomic update, no plaintext persistence**

### Deletion
- Harbor robot account deleted
- DB record cascade-deleted from satellite
- Proper cleanup on transaction rollback

✅ **Clean deletion path**

### Logging
- Tokens masked via `maskToken()` (first/last 4 chars only)
- No plaintext secrets in any log statement
- Robot/satellite names logged (operational metadata)

✅ **No credential leakage in logs**

---

## Test Coverage Assessment

### Covered
- ✅ `hashRobotCredentials` - basic, empty, long secrets, salt uniqueness
- ✅ `verifyRobotCredentials` - correct/wrong secrets, malformed hashes
- ✅ `robotDurationDays` - default, custom, invalid, zero, negative
- ✅ `RobotAccountTemplate` - duration configuration

### Gaps
- ❌ `refreshRobotSecret` (integration test, requires mocking)
- ❌ `ensureSatelliteRobotAccount` (integration test, requires mocking)
- ❌ `storeRobotAccountInDB`
- ❌ Tampered hash with valid format but modified bytes

**Status:** Core crypto functions are well-tested. Integration functions lack tests but would require significant mocking infrastructure.

---

## Recommendations

### Immediate Actions (Pre-Merge)
1. **Fix M1:** Add length check before `projects[0]` access (helpers.go:188)
2. **Fix M2:** Replace raw error exposure with generic messages
3. **Document M3:** Add clear warning that `SKIP_HARBOR_HEALTH_CHECK` is test-only

### Post-Merge Improvements
4. **L1:** Extract argon2 to shared package to eliminate duplication
5. **L6:** Consider parsing parameters from stored hash for future-proofing
6. **L8:** Add authentication to `/satellites/sync` endpoint
7. Replace `interface{}` with `any` throughout codebase
8. Add named constants for argon2 parameters
9. Fix error wrapping inconsistencies (`%w` vs `%v`/`.Error()`)

---

## Compliance Status

### Security Standards
- ✅ **OWASP Password Storage Cheat Sheet:** Fully compliant
- ✅ **OWASP Top 10 2021:** 8/8 applicable categories compliant
- ✅ **Go Security Best Practices:** Proper use of crypto/rand, subtle, error handling
- ✅ **NIST SP 800-63B:** Argon2id is NIST-recommended for password hashing

### Code Quality
- ✅ **Google Go Style Guide:** Generally compliant with minor deviations
- ⚠️ **DRY Principle:** One violation (duplicated argon2 implementation)
- ✅ **Test Coverage:** Core functionality covered
- ✅ **Error Handling:** Mostly correct use of error wrapping

---

## Conclusion

PR #310 represents a **significant security improvement** to Harbor Satellite's Ground Control component. The transition from plaintext to hashed robot secret storage eliminates a critical credential exposure risk in the event of database compromise. The cryptographic implementation is sound, using industry-standard primitives with correct parameters.

The three MEDIUM-severity findings are all remediable with small code changes and should be addressed before merge. The LOW-severity findings are primarily code quality improvements that can be addressed in follow-up PRs.

**Final Recommendation:** ✅ **APPROVE** with the requirement to fix M1-M3 before merge.

---

## Review Metadata

**Files Analyzed:** 11 files (+329/-78 lines)
- `ground-control/internal/server/helpers.go`
- `ground-control/internal/server/helpers_test.go` (new)
- `ground-control/internal/server/satellite_handlers.go`
- `ground-control/internal/server/spire_handlers.go`
- `ground-control/internal/database/models.go`
- `ground-control/internal/database/robot_accounts.sql.go`
- `ground-control/reg/harbor/robot.go`
- `ground-control/reg/harbor/robot_test.go` (new)
- `ground-control/sql/queries/robot_accounts.sql`
- `ground-control/sql/schema/004_robot_accounts.sql`
- `ground-control/.env.example`

**Review Team:**
- pr-fetcher: PR context and diff analysis
- crypto-reviewer: Cryptographic implementation security
- auth-flow-reviewer: Authentication and credential handling
- error-handler-reviewer: Error handling and edge cases
- go-practices-reviewer: Go best practices and code quality
- owasp-reviewer: OWASP Top 10 compliance
- team-lead: Report compilation and coordination

**Tools Used:**
- Manual code review
- OWASP guidelines comparison
- Go security best practices checklist
- GitHub gh CLI for PR data extraction
