/*******************************************************************************
*   (c) 2018 ZondaX GmbH
*
*  Licensed under the Apache License, Version 2.0 (the "License");
*  you may not use this file except in compliance with the License.
*  You may obtain a copy of the License at
*
*      http://www.apache.org/licenses/LICENSE-2.0
*
*  Unless required by applicable law or agreed to in writing, software
*  distributed under the License is distributed on an "AS IS" BASIS,
*  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*  See the License for the specific language governing permissions and
*  limitations under the License.
********************************************************************************/

package ledger_gitopia_go

import (
	"fmt"
	"math"

	ledger_go "github.com/cosmos/ledger-go"
)

const (
	userCLA = 0x55

	userINSGetVersion       = 0
	userINSSignSECP256K1    = 2
	userINSGetAddrSecp256k1 = 4

	userMessageChunkSize = 250
)

// LedgerCosmos represents a connection to the Cosmos app in a Ledger Nano S device
type LedgerCosmos struct {
	appName string
	api     *ledger_go.Ledger
	version VersionInfo
}

// FindLedgerCosmosUserApp finds a Cosmos user app running in a ledger device
func FindLedgerCosmosUserApp() (*LedgerCosmos, error) {
	ledgerAPI, err := ledger_go.FindLedger()

	if err != nil {
		return nil, err
	}

	app := LedgerCosmos{"",ledgerAPI, VersionInfo{}}
	err = app.LoadAppName()

	if err != nil {
		defer ledgerAPI.Close()
		if err.Error() == "[APDU_CODE_CLA_NOT_SUPPORTED] Class not supported" {
			return nil, fmt.Errorf("are you sure the Gitopia or Cosmos app is open?")
		}
		return nil, err
	}

	err = app.LoadVersion()
	if err != nil {
		defer ledgerAPI.Close()
		if err.Error() == "[APDU_CODE_CLA_NOT_SUPPORTED] Class not supported" {
			return nil, fmt.Errorf("are you sure the Gitopia or Cosmos app is open?")
		}
		return nil, err
	}

	err = app.CheckVersion()
	
	if err != nil {
		defer ledgerAPI.Close()
		return nil, err
	}

	return &app, err
}

// Close closes a connection with the Cosmos user app
func (ledger *LedgerCosmos) Close() error {
	return ledger.api.Close()
}

// VersionIsSupported returns true if the App version is supported by this library
func (ledger *LedgerCosmos) CheckVersion() error {
	// ver := ledger.version
	// appName := ledger.appName
	// if appName == "Gitopia" {
	// 	return CheckVersion(ver, VersionInfo{0, 0, 1, 0})
	// } else if appName == "Cosmos" {
	// 	return CheckVersion(ver, VersionInfo{0, 2, 1, 0})
	// }

	// return fmt.Errorf("App version is not supported")
	return nil
}

// GetVersion returns the current version of the Cosmos user app
func (ledger *LedgerCosmos) LoadVersion() error {
	message := []byte{userCLA, userINSGetVersion, 0, 0, 0}
	response, err := ledger.api.Exchange(message)

	if err != nil {
		return err
	}

	if len(response) < 4 {
		return fmt.Errorf("invalid response")
	}

	ledger.version = VersionInfo{
		AppMode: response[0],
		Major:   response[1],
		Minor:   response[2],
		Patch:   response[3],
	}

	return nil
}

func (ledger *LedgerCosmos) LoadAppName() error {
	message := []byte{0xb0, 0x01, 0, 0, 0}
	response, err := ledger.api.Exchange(message)
	if err != nil {
		return err
	}

	if response[0] != 1 {
		return fmt.Errorf("response format ID not recognized")
	}

	idx := 2
	appNameLen := int(response[1])
	appName := string(response[idx : idx+appNameLen])
	ledger.appName = appName
	return nil
}


// SignSECP256K1 signs a transaction using Cosmos user app
// this command requires user confirmation in the device
func (ledger *LedgerCosmos) SignSECP256K1(bip32Path []uint32, transaction []byte) ([]byte, error) {
	return ledger.sign(bip32Path, transaction)
}

// GetPublicKeySECP256K1 retrieves the public key for the corresponding bip32 derivation path (compressed)
// this command DOES NOT require user confirmation in the device
func (ledger *LedgerCosmos) GetPublicKeySECP256K1(bip32Path []uint32) ([]byte, error) {
	pubkey, _, err := ledger.getAddressPubKeySECP256K1(bip32Path, "gitopia", false)
	return pubkey, err
}

func validHRPByte(b byte) bool {
	// https://github.com/bitcoin/bips/blob/master/bip-0173.mediawiki
	return b >= 33 && b <= 126
}

// GetAddressPubKeySECP256K1 returns the pubkey (compressed) and address (bech(
// this command requires user confirmation in the device
func (ledger *LedgerCosmos) GetAddressPubKeySECP256K1(bip32Path []uint32, hrp string) (pubkey []byte, addr string, err error) {
	return ledger.getAddressPubKeySECP256K1(bip32Path, hrp, true)
}

func (ledger *LedgerCosmos) GetBip32bytes(bip32Path []uint32, hardenCount int) ([]byte, error) {
	var pathBytes []byte
	var err error
	// check
	if (ledger.appName == "Gitopia") {
		pathBytes, err = GetBip32bytesv2(bip32Path, 3)
		if err != nil {
			return nil, err
		}
		} else {
			return nil, fmt.Errorf("App version is not supported")
	}

	return pathBytes, nil
}


func (ledger *LedgerCosmos) sign(bip32Path []uint32, transaction []byte) ([]byte, error) {
	var packetIndex byte = 1
	var packetCount = 1 + byte(math.Ceil(float64(len(transaction))/float64(userMessageChunkSize)))

	var finalResponse []byte

	var message []byte

	for packetIndex <= packetCount {
		chunk := userMessageChunkSize
		if packetIndex == 1 {
			pathBytes, err := ledger.GetBip32bytes(bip32Path, 3)
			if err != nil {
				return nil, err
			}
			header := []byte{userCLA, userINSSignSECP256K1, 0, 0, byte(len(pathBytes))}
			message = append(header, pathBytes...)
		} else {
			if len(transaction) < userMessageChunkSize {
				chunk = len(transaction)
			}

			payloadDesc := byte(1)
			if packetIndex == packetCount {
				payloadDesc = byte(2)
			}

			header := []byte{userCLA, userINSSignSECP256K1, payloadDesc, 0, byte(chunk)}
			message = append(header, transaction[:chunk]...)
		}

		response, err := ledger.api.Exchange(message)
		if err != nil {
			if err.Error() == "[APDU_CODE_BAD_KEY_HANDLE] The parameters in the data field are incorrect" {
				// In this special case, we can extract additional info
				errorMsg := string(response)
				switch errorMsg {
				case "ERROR: JSMN_ERROR_NOMEM":
					return nil, fmt.Errorf("Not enough tokens were provided")
				case "PARSER ERROR: JSMN_ERROR_INVAL":
					return nil, fmt.Errorf("Unexpected character in JSON string")
				case "PARSER ERROR: JSMN_ERROR_PART":
					return nil, fmt.Errorf("The JSON string is not a complete.")
				}
				return nil, fmt.Errorf(errorMsg)
			}
			if err.Error() == "[APDU_CODE_DATA_INVALID] Referenced data reversibly blocked (invalidated)" {
				errorMsg := string(response)
				return nil, fmt.Errorf(errorMsg)
			}
			return nil, err
		}

		finalResponse = response
		if packetIndex > 1 {
			transaction = transaction[chunk:]
		}
		packetIndex++

	}
	return finalResponse, nil
}

// GetAddressPubKeySECP256K1 returns the pubkey (compressed) and address (bech(
// this command requires user confirmation in the device
func (ledger *LedgerCosmos) getAddressPubKeySECP256K1(bip32Path []uint32, hrp string, requireConfirmation bool) (pubkey []byte, addr string, err error) {
	if len(hrp) > 83 {
		return nil, "", fmt.Errorf("hrp len should be <10")
	}

	hrpBytes := []byte(hrp)
	for _, b := range hrpBytes {
		if !validHRPByte(b) {
			return nil, "", fmt.Errorf("all characters in the HRP must be in the [33, 126] range")
		}
	}

	pathBytes, err := ledger.GetBip32bytes(bip32Path, 3)
	if err != nil {
		return nil, "", err
	}

	p1 := byte(0)
	if requireConfirmation {
		p1 = byte(1)
	}

	// Prepare message
	header := []byte{userCLA, userINSGetAddrSecp256k1, p1, 0, 0}
	message := append(header, byte(len(hrpBytes)))
	message = append(message, hrpBytes...)
	message = append(message, pathBytes...)
	message[4] = byte(len(message) - len(header)) // update length

	response, err := ledger.api.Exchange(message)

	if err != nil {
		return nil, "", err
	}
	if len(response) < 35+len(hrp) {
		return nil, "", fmt.Errorf("Invalid response")
	}

	pubkey = response[0:33]
	addr = string(response[33:])

	return pubkey, addr, err
}
