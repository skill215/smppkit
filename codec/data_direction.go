package codec

import (
	"fmt"

	"github.com/skill215/smppkit/pdu"
	"github.com/skill215/smppkit/tlv"
)

// DataDirection describes the original data_sm request, including when checking
// its data_sm_resp. A response travels in the opposite transport direction.
type DataDirection uint8

const (
	DataSubmission DataDirection = iota + 1 // ESME -> MC request; MC -> ESME response
	DataDelivery                            // MC -> ESME request; ESME -> MC response
)

// ValidateDataDirection adds contextual checks for DataSM/DataSMResp to an
// ordinary Append or Decode. Call it in addition to normal encoding/decoding:
// it does not replace framing, width, value, dependency or customer validation.
// Without this explicit context, Codec continues to accept the direction union.
// Omitted data_sm_resp is supported; other bodies, nil and unknown directions
// are rejected. Unknown/customer tags have no inferred direction restriction.
// For a response with original-request mode context, use ValidateDataResponse.
// This method is read-only, concurrency safe and never invokes user callbacks.
func (c *Codec) ValidateDataDirection(body pdu.Body, direction DataDirection) error {
	if nilBody(body) || direction != DataSubmission && direction != DataDelivery {
		return invalidField("data_sm direction or body")
	}
	if c.version == pdu.V33 {
		return ErrUnsupported
	}
	var fields tlv.Fields
	response := false
	switch b := body.(type) {
	case *pdu.DataSM:
		fields = b.Optional
		// Datagram submission has no registered-delivery service (3.4 §2.10.2,
		// 5.0 §4.2.10.3). Delivery mode bits do not identify the original
		// submission mode, and mode zero leaves the MC's default unspecified.
		if direction == DataSubmission && b.ESMClass&3 == 1 && b.RegisteredDelivery != 0 {
			return invalidField("data_sm.registered_delivery in datagram mode")
		}
		// 3.4 §5.2.12 has distinct ESME->SMSC and SMSC->ESME type tables.
		// 5.0 §4.7.12 explicitly permits receipt/intermediate types on submission.
		if !c.validESMClass(b.ESMClass, pdu.DataSMID) || c.version == pdu.V34 && direction == DataSubmission && !c.validESMClass(b.ESMClass, pdu.SubmitSMID) {
			return invalidField("data_sm.esm_class direction")
		}
	case *pdu.DataSMResp:
		fields, response = b.Optional, true
	case *pdu.Omitted:
		if b.ID != pdu.DataSMRespID {
			return invalidField("data_sm response body")
		}
		return nil // status/omission legality still belongs to Append/Decode
	default:
		return invalidField("data_sm direction body")
	}
	// Even standalone contextual checks have a finite scan bound. Full frame
	// accounting (including value bytes) remains the normal Codec's job.
	if len(fields) > c.max/4 {
		return invalidField("data_sm optional field count")
	}
	for _, field := range fields {
		allowed := true
		if c.version == pdu.V50 {
			// Reuse the standard submission/delivery tables rather than copying a
			// second catalog. data_sm_resp uses the original request's direction.
			mask := submitTLV
			if direction == DataDelivery {
				mask = deliverTLV
			}
			if response {
				mask = submitResponseTLV
				if direction == DataDelivery {
					mask = deliverResponseTLV
				}
			}
			if def, known := standardTLVs[field.Tag]; known {
				allowed = def.v50&mask != 0
			}
		} else if response {
			// 3.4 §§5.3.2.28/29 describe the SMSC reporting its delivery flag.
			allowed = direction != DataDelivery || field.Tag != tlv.DPFResult
		} else {
			// 3.4 uses one data_sm TLV table. Apply its explicit directional
			// descriptions, not 5.0's narrower delivery table (e.g. TTL stays valid).
			switch field.Tag {
			case tlv.ReceiptedMessageID, tlv.MessageState, tlv.NetworkErrorCode:
				allowed = direction == DataDelivery
			case tlv.SetDPF, tlv.MoreMessagesToSend:
				allowed = direction == DataSubmission
			}
		}
		if !allowed {
			return fmt.Errorf("%w: data_sm TLV 0x%04x direction", ErrValidation, field.Tag)
		}
	}
	return nil
}
