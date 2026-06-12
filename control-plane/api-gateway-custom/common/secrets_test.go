// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateKeyLength(t *testing.T) {
	tooShortPrivateKey := `-----BEGIN RSA PRIVATE KEY-----
MIICXAIBAAKBgQCtmK1VjmXJ7vm4CZkkOSjc+kjGNMlyce5rXxwlDRz9LcGGc3Tg
kwUJesyBpDtxLLVHXQIPr5mWYbX/W/ezQ9sntxrATbDek8pBgoOlARebwkD2ivVW
BWfVhlryVihWlXApKiJ2n3i0m+OVtdrceC9Bv2hEMhYVOwzxtb3O0YFkbwIDAQAB
AoGAIxgnipFUEKPIRiVimUkY8ruCdNd9Fi7kNT6wEOl6v9A9PHIg4bm3Hfh+WYMb
JUEVkMzDuuoUEavFQE+WXt5L8oE1lEBmN2++FQsvllN+MRBTRg2sfw4mUWDI6S4r
h8+XNTzTIg2sUd2J3o2qNmQoOheYb+iuYDj76IFoEdwwZ0kCQQDYKKs5HAbnrLj1
UrOp8TyHdFf0YNw5tGdbNTbffq4rlBD6SW70+Sj624i2UqdnYwRiWzdXv3zN08aI
Vfoh2cGlAkEAzZe5B6BhiX/PcIYutMtuT3K+mysFNlowrutXWoQOpR7gGAkgEt6e
oCDgx1QJRjsp6NFQxKc6l034Hzs17gqJgwJAcu9U873aUg9+HTuHOoKB28haCCAE
mU46cr3d2oKCW7uUN3EaZXmid5iJneBfENMOfrnfuHGiC9NiShXlNWCS3QJAO5Ne
w83+1ahaxUGs4SkeExmuECrcPM7P0rBRxOIFmGWlDHIAgFdQYhiE6l34vghA8b1O
CV5oRRYL84jl7M/S3wJBALDfL5YXcc8P6scLJJ1biqhLYppvGN5CUwbsJsluvHCW
XCTVIbPOaS42A0xUfpoiTcdbNSFRvdCzPR5nsGy8Y7g=
-----END RSA PRIVATE KEY-----`
	validPrivateKey := `-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEAzVKRcYlTHHPjPbCieOFIUT2hCouRYe4N8ZhNrSpZf/BAAn4M
d/LWn/9OrLagbxrRF6cWdWGNEI2COnBRLgNVxyPXneaHaYFqOBRi9GWhuD3sw1jn
7gf4/m/AVO8cu2JYjEX+s9RjSRzpjx+4nhit46bGNUyb9qUeQwoBidAzOSmU8nHY
y3LpuuzkjS3FEyNXHxqgpTJnV4ytx8YGkPnG92GBAlrZnr4Eclv0/Sq6OViTpeuh
z8noNkbugYWHMXGlTZ4lPnELJW2fx/HIpD2ovOO3X8XYBo5KDzs9qyKzDgIOMZLF
i/qLCLHgfosb4TMaXCeVu4fA7Y47jtGOO4mbiwIDAQABAoIBAFhicDibIDtRyaLv
K+l0NPC/4liLPwCUfM0gvmNKJS/VSICqKQzjbK+ANCpWDVb2iMaxRxItdY+IEuS8
H736cozgaXtP1r+8lXBhmj1RmJ2ajpaC6YgGR5GjonwNWGVzjuGHaf6YcUryVrol
MhBgWE50psMf4M16Q74hCwt7o+k5Lz55xKasgc9dtSnvyCupPBwrOT+d55C1P2Wn
2oebWM4WKtCZIgvlvZrt4xQkGWy9qloxL6V1F67ZbizAyFMZUMmJv+4/whF8tmXi
aydleL64K23ZSK1pM/x0JI+7qo0GpEoA4k+2fdmh5dAOM0TrXhV5Kv01efLIaITT
s7lYjG0CgYEA4qGIM7qO3e9fHgSK/9UdxnpL/1OvfYATBMhEtR46sAxmKQGC8fTM
iTBkmLAKn3zBgDghCbygPIQjex+W+Ra7JkQIcGB6KLR8rr5GkOuF6vkqHV93RQRT
lT/1quqq3fVH6V4ymifKJCDNg0IEPcmo+M8RnXBgpFsCN4b5UyjXNScCgYEA5+4h
LITPJxGytlWzwtsy44U2PvafJYJCktW+LYqhk3xzz4qWX5ubmPz18LrEyybgcy/W
Dm4JCu+TOS2gvf2WbJKR/tKdgRN7dkU/dbgMtRL8QW5ir+5qqRITYOhiSZPIOpbP
5zg+c/ZvmK/t5h35/8l7b0bu/E1FOEF27ADpzP0CgYEArqch2gup0muI+A80N9i7
q5vQOaL6mVM8VPEp0hLL06Sajnt1uJWZkxhSTkFMzoBMd03KWECflEOZPGep56iW
7fR8NG6Fdh0yAVDt/P0lJWKEDELoHa4p49l4sBFNQOSoWLaZdKe5ZoJJHyCfOCbT
K3wY7SYPtFnWqYhBWM8emv0CgYBdrNqNRp78orNR3c+bNjmZl6ZPTAD/f1swP1Bu
yH12Ol/0RX9y4kC4TANx1Z3Ch9ND8uA8N8lDN3x5Laqs0g29kH2TNLIU/i9xl4qI
G2xWfnKQYutNL7i4zOoyy+lW2m+W6m7Sbu8am0B7pSMrPJRK8a//Q+Em2nbIv/gu
XjgQaQKBgHKZUKkMv597vpAjgTNsKIl5RDFONBq3omnAwlK9EDLVeAxIrvrvMHBW
H/ZMFpSGp1eQgKyu1xkEqGdkYXx7BKtdTHK+Thqif2ZGWczy5rVSAIsBYDo1DGE2
wbocWxkWNb5o2ZZtis5lTB6nr9EWo0zyaPqIh0pfjqVEES2YDEx6
-----END RSA PRIVATE KEY-----`
	nonTraditionalRSAKey := `-----BEGIN PRIVATE KEY-----
MIIEvAIBADANBgkqhkiG9w0BAQEFAASCBKYwggSiAgEAAoIBAQCcrB9oNKLtzA3Q
02KDgtsnrxns7vJ5aCkjJCm/h0Ju7a2mel5YHSN5iLlU5oTMJVIMpWlW9E8P76/a
GLGMNfSBRVJdfW71iks/ddp4SjpDe9Bo+aY2snrR2/AP7eQepVNjFbg4YLQqvENh
05k1FuuP1/AgGVNn0kGEwzKxz35shmhRKBCvaRaHLz/fdkDIeIrVLON4FnmAmpOZ
AztZCwAZc6HZfj8Nh9Wlaw6Dg2boIgxTU160pwpX+nUxcJ9M5sUP9DBuNL0Mdrqi
U+R49uqG/5ssSk+xVik3q+WF+XySJ6H21fttWDJS2OTm/Nx/wHlBC73mthbA0emB
rkiBy9SBAgMBAAECggEAOhybz6aKcmKYE0d8yGPejwMjPh9JH+ATNh4hQBHXAdc1
7ESCPvOb52XfvE5+nkwPeXJXNrIKq1IPq3kyTdvrc5F3Ygb3A6tGiuTXYnvBzasc
m/tRfANKjBGkovvte7J90ghJ2tt/qERJR/1Y2/jC6glB314VcjJqK+jNImfgsDa7
1r47efKG7B5eUGvhQDTpL5ENXKxIdvCghHrLqj19QGUZ5MbXsEYrso0lxKw2Xk39
uM8p3WTxIy0LQGyCm+FYlJ7r61tm7tUOGuNT0YiptVavIw1QPgIbRWdS2gnJu3+J
kHS0vu6AW1fJav48TA9hXcIQR70alrJA2VVqsvQouwKBgQDNs96l8BfWD6s/urIw
yzC3/VZPLFJ3BlxvkdP1UDC0S+7pgQ6qdEmJg0z5IfYzDB1PK2X/DS/70JA1LRSS
MRmjQGHCYIp9g8EqmABwfKf4YnN53KPRyR8Yq1pwaq7wKowtW+5GH95qQPINZsNO
J21AENEzq7IoB4gpM3tIaX73YwKBgQDC+yl5JvoV7e6FIpFrwL62aKrWmpidML/G
stdrg9ylCSM9SIVFINMhmFPicW1+DrkQ5HRV7DG//ZcOZNbbNmSu32PVcQI1MJgQ
rkMZ3ukUURnlvQYOEmZY4zHzTJ+jcw6kEH/+b47Bv13PpD7ZqA4/28dpU9wi9gt3
+GiSnkKDywKBgHqjr63dPEjapK3lQFHJAu3fM7MWaMAf4cJ+/hD202LbFsDOuhC0
Lhe3WY/7SI7cvSizZicvFJmcmi2qB+a1MWTcgKxj5I26nNMpNrHaEEcNY22XN3Be
6ZRKrSvy3wO/Sj3M3n2eiHtu5yFIUE7rQL5+iEu3JQuqmep+kBT3GMSjAoGAP77B
VlyJ0nWRT3F3vZSsRRJ/F94/GtT/PcTmbL4Vetc78CMvfuQ2YntcoWGX/Ghv1Lf7
2MN5mF0d75TEMbLcw9dA2l0x7ZXPgVSXl3OrG/tPzi44No2JbHIKuJJKdrN9C+Jh
Fhv+vhUEZIg8DAjHb9U4opTKGZv7L+PEvHqFIHUCgYBTB2TxTgEMNZSsRwrhQRMh
tsz5rS2MoTgzk4BlSsv6xVC4GnBJ2HlNAjYEsBEg50zCCTPlZXcsNjrAxFrwWhLJ
DjN2iMsYFz4WHS94W5UYl6/35ye25KsHuS9vnNeidhFAvYgC1nIkh4mFhLoSeSCG
GODy2KwC2ssLuUHb6WoJ6A==
-----END PRIVATE KEY-----`

	testCases := map[string]struct {
		key           string
		expectedError error
	}{
		"key is RSA and of the correct length": {
			key:           validPrivateKey,
			expectedError: nil,
		},
		"key is RSA and too short": {
			key:           tooShortPrivateKey,
			expectedError: errKeyLengthTooShort,
		},
		"key is non-traditional RSA key": {
			key:           nonTraditionalRSAKey,
			expectedError: nil,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			err := ValidateKeyLength(tc.key)
			require.ErrorIs(t, err, tc.expectedError)
		})
	}
}
