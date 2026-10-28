package codec

import (
	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/tlv"
)

// ValidateDataResponse supplements ordinary Append/Decode with the original
// data_sm request's ESM class and direction. It includes ValidateDataDirection.
// Save requestESMClass before handing an owned request to application code.
// This does not validate framing, sequence correlation, field widths, or customer
// definitions; callers must still use Append/Decode. No user callbacks run here.
// A zero mode means an unknown MC default, not necessarily store-and-forward.
// Delivery requests do not reveal the original submission's messaging mode.
func (c *Codec) ValidateDataResponse(response pdu.PDU, requestESMClass byte, direction DataDirection) error {
	// Validate the context even for omitted responses and Generic NACK. The
	// request's flags/TLVs were checked separately when it was sent or received.
	if err := c.ValidateDataDirection(&pdu.DataSM{ESMClass: requestESMClass}, direction); err != nil {
		return err
	}
	if nilBody(response.Body) {
		return invalidField("data_sm response body")
	}
	if _, ok := response.Body.(*pdu.GenericNACK); ok {
		return nil // ordinary Codec owns the nonzero-status requirement
	}
	switch response.Body.(type) {
	case *pdu.DataSMResp, *pdu.Omitted:
	default:
		return invalidField("data_sm response body")
	}
	if err := c.ValidateDataDirection(response.Body, direction); err != nil {
		return err
	}
	body, present := response.Body.(*pdu.DataSMResp)
	if !present || direction != DataSubmission {
		return nil
	}
	mode := requestESMClass & 3
	for _, field := range body.Optional {
		transactionOnly := false
		switch field.Tag {
		case tlv.DPFResult, tlv.DeliveryFailureReason, tlv.NetworkErrorCode:
			transactionOnly = true // 3.4 Table 4-21 footnote a; 5.0 §4.2.5
		case tlv.AdditionalStatusInfoText:
			transactionOnly = c.version == pdu.V50
		}
		// Zero cannot be resolved without peer configuration. Congestion and
		// unknown/customer parameters have no inferred transaction restriction.
		if transactionOnly && (mode == 1 || mode == 3) {
			return invalidField("data_sm_resp TLV requires transaction mode")
		}
		// Only explicit transaction submission makes OK a synchronous delivery
		// outcome. Do not infer delivery success from a generic accepted submit.
		// Both editions explicitly exclude failure_reason on successful delivery.
		if mode == 2 && response.Status == pdu.OK && field.Tag == tlv.DeliveryFailureReason {
			return invalidField("data_sm_resp failure reason on successful transaction")
		}
	}
	return nil
}
