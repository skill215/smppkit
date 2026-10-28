// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Library code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-28

package codec

import (
	"bytes"

	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/tlv"
)

// validStandardValue is called only after version-specific width checks. It
// validates protocol-reserved bits/ranges, not external network policy or text
// alphabets. Network-specific error codes, USSD vendor operations, language
// extensions and payload bytes deliberately retain their opaque values.
func validStandardValue(tag uint16, v []byte, version pdu.Version) bool {
	switch tag {
	case tlv.DestAddrSubunit, tlv.SourceAddrSubunit:
		return v[0] <= 4
	case tlv.DestNetworkType, tlv.SourceNetworkType, tlv.DestBearerType, tlv.SourceBearerType:
		return v[0] <= 8
	case tlv.DestTelematicsID:
		return v[1] == 0 // reserved second octet
	case tlv.PayloadType, tlv.DPFResult, tlv.SetDPF, tlv.MoreMessagesToSend:
		return v[0] <= 1
	case tlv.AdditionalStatusInfoText:
		return cOctets(v) && ascii(v)
	case tlv.ReceiptedMessageID:
		return cOctets(v) && ascii(v)
	case tlv.SourceNetworkID, tlv.DestNetworkID:
		return cOctets(v) && ascii(v)
	case tlv.MSMsgWaitFacilities:
		return v[0]&0x7c == 0
	case tlv.PrivacyIndicator, tlv.DeliveryFailureReason:
		return v[0] <= 3
	case tlv.SourceSubaddress, tlv.DestSubaddress:
		return v[0] == 0x80 || v[0] == 0x88 || v[0] == 0xa0
	case tlv.CallbackNumPresInd:
		return v[0]&0xf0 == 0 && v[0]&0x0c != 0x0c
	case tlv.CallbackNum:
		return v[0] <= 1 && v[1] <= 6 && validNPI(v[2])
	case tlv.CallbackNumATag:
		return validDataCoding(v[0], version)
	case tlv.NumberOfMessages:
		return v[0] <= 99
	case tlv.SCInterfaceVersion:
		return pdu.Version(v[0]).Valid()
	case tlv.MSAvailabilityStatus, tlv.DisplayTime, tlv.DestAddrNPResolution:
		return v[0] <= 2
	case tlv.NetworkErrorCode:
		max := byte(3)
		if version == pdu.V50 {
			max = 8
		}
		return v[0] >= 1 && v[0] <= max
	case tlv.MessageState:
		if version == pdu.V50 {
			return v[0] <= 9
		}
		return v[0] >= 1 && v[0] <= 8
	case tlv.CongestionState:
		return v[0] <= 100
	case tlv.BillingIdentification:
		return v[0]&0x80 != 0
	case tlv.SourceNodeID, tlv.DestNodeID, tlv.DestAddrNPInformation:
		for _, digit := range v {
			if digit < '0' || digit > '9' {
				return false
			}
		}
		return true
	case tlv.USSDServiceOp:
		return v[0] <= 3 || v[0] >= 16 && v[0] <= 19 || v[0] >= 32
	case tlv.AlertOnMessageDelivery:
		return len(v) == 0 || version == pdu.V50 && v[0] <= 3
	case tlv.MSValidity:
		if len(v) == 1 {
			return v[0] <= 3
		}
		return version == pdu.V50 && len(v) == 4 && v[0] == 4 && v[1] <= 6
	case tlv.ITSReplyType:
		return v[0] <= 8
	}
	return true
}
func cOctets(v []byte) bool {
	return len(v) > 0 && v[len(v)-1] == 0 && bytes.IndexByte(v[:len(v)-1], 0) < 0
}
func ascii(v []byte) bool {
	for _, b := range v {
		if b > 127 {
			return false
		}
	}
	return true
}
func validNPI(v byte) bool {
	switch v {
	case 0, 1, 3, 4, 6, 8, 9, 10, 14, 18:
		return true
	}
	return false
}
