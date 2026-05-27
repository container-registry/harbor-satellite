// Copyright 2021 Contributors to the Parsec project.
// SPDX-License-Identifier: Apache-2.0

package parsec

import (
	"github.com/parallaxsecond/parsec-client-go/interface/operations/listproviders"
	"github.com/parallaxsecond/parsec-client-go/interface/requests"
)

// ProviderID for providers
type ProviderID uint8

// Provider IDs (uint8 wire-protocol values, not 128-bit UUIDs).
const (
	ProviderCore           ProviderID = 0
	ProviderMBed           ProviderID = 1
	ProviderPKCS11         ProviderID = 2
	ProviderTPM            ProviderID = 3
	ProviderTrustedService ProviderID = 4
	ProviderCryptoAuthLib  ProviderID = 5
)

// HasCrypto returns true if the provider supports crypto
func (p *ProviderID) HasCrypto() bool {
	return *p != ProviderCore
}

func (p ProviderID) String() string {
	switch p {
	case ProviderCore:
		return "Core"
	case ProviderMBed:
		return "MBed"
	case ProviderPKCS11:
		return "PKCS11"
	case ProviderTPM:
		return "TPM"
	case ProviderTrustedService:
		return "TrustedService"
	case ProviderCryptoAuthLib:
		return "CryptoAuthLib"
	default:
		return "Unknown"
	}
}

type ProviderInfo struct {
	UUID        string
	Description string
	Vendor      string
	VersionMaj  uint32
	VersionMin  uint32
	VersionRev  uint32
	ID          ProviderID
}

func newProviderIDFromOp(p requests.ProviderID) ProviderID {
	return ProviderID(p)
}

func newProviderInfoFromOp(inf *listproviders.ProviderInfo) *ProviderInfo {
	return &ProviderInfo{
		UUID:        inf.Uuid,
		Description: inf.Description,
		Vendor:      inf.Vendor,
		VersionMaj:  inf.VersionMaj,
		VersionMin:  inf.VersionMin,
		VersionRev:  inf.VersionRev,
		ID:          newProviderIDFromOp(requests.ProviderID(inf.Id)),
	}
}
