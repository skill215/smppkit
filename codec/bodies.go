package codec

import (
	"fmt"
	"github.com/skill215/smppkit/internal/wire"
	"github.com/skill215/smppkit/pdu"
)

func (c *Codec) decodeBody(id pdu.CommandID, b []byte) (pdu.Body, error) {
	r := &wire.Reader{Data: b}
	var body pdu.Body
	switch id {
	case pdu.BindReceiverID:
		body = &pdu.BindReceiver{BindFields: c.readBind(r)}
	case pdu.BindReceiverRespID:
		v := pdu.BindResponseFields{SystemID: r.C(16, "system_id")}
		v.Optional = c.readTLVs(r, id)
		body = &pdu.BindReceiverResp{BindResponseFields: v}
	case pdu.BindTransmitterID:
		body = &pdu.BindTransmitter{BindFields: c.readBind(r)}
	case pdu.BindTransmitterRespID:
		v := pdu.BindResponseFields{SystemID: r.C(16, "system_id")}
		v.Optional = c.readTLVs(r, id)
		body = &pdu.BindTransmitterResp{BindResponseFields: v}
	case pdu.BindTransceiverID:
		body = &pdu.BindTransceiver{BindFields: c.readBind(r)}
	case pdu.BindTransceiverRespID:
		v := pdu.BindResponseFields{SystemID: r.C(16, "system_id")}
		v.Optional = c.readTLVs(r, id)
		body = &pdu.BindTransceiverResp{BindResponseFields: v}
	case pdu.SubmitMultiID:
		body = c.readMulti(r)
	case pdu.SubmitMultiRespID:
		body = c.readMultiResp(r)
	case pdu.SubmitSMID:
		body = &pdu.SubmitSM{SMFields: c.readSM(r, id)}
	case pdu.DeliverSMID:
		body = &pdu.DeliverSM{SMFields: c.readSM(r, id)}
	case pdu.SubmitSMRespID:
		v := pdu.MessageResponseFields{MessageID: r.C(c.messageIDLimit(), "message_id")}
		v.Optional = c.readTLVs(r, id)
		body = &pdu.SubmitSMResp{MessageResponseFields: v}
	case pdu.DeliverSMRespID:
		v := pdu.MessageResponseFields{MessageID: r.C(c.messageIDLimit(), "message_id")}
		v.Optional = c.readTLVs(r, id)
		body = &pdu.DeliverSMResp{MessageResponseFields: v}
	case pdu.DataSMRespID:
		v := pdu.MessageResponseFields{MessageID: r.C(c.messageIDLimit(), "message_id")}
		v.Optional = c.readTLVs(r, id)
		body = &pdu.DataSMResp{MessageResponseFields: v}
	case pdu.UnbindID:
		body = &pdu.Unbind{}
	case pdu.UnbindRespID:
		body = &pdu.UnbindResp{Optional: c.readTLVs(r, id)}
	case pdu.EnquireLinkID:
		body = &pdu.EnquireLink{}
	case pdu.EnquireLinkRespID:
		body = &pdu.EnquireLinkResp{Optional: c.readTLVs(r, id)}
	case pdu.ReplaceSMRespID:
		body = &pdu.ReplaceSMResp{Optional: c.readTLVs(r, id)}
	case pdu.CancelSMRespID:
		body = &pdu.CancelSMResp{Optional: c.readTLVs(r, id)}
	case pdu.GenericNACKID:
		body = &pdu.GenericNACK{Optional: c.readTLVs(r, id)}
	case pdu.QuerySMID:
		body = &pdu.QuerySM{MessageID: r.C(c.messageIDLimit(), "message_id"), Source: c.readAddr(r, c.addressLimit(id), "source_addr")}
	case pdu.QuerySMRespID:
		body = &pdu.QuerySMResp{MessageID: r.C(c.messageIDLimit(), "message_id"), FinalDate: r.C(17, "final_date"), MessageState: r.U8("message_state"), ErrorCode: r.U8("error_code"), Optional: c.readTLVs(r, id)}
	case pdu.CancelSMID:
		body = &pdu.CancelSM{ServiceType: r.C(6, "service_type"), MessageID: r.C(c.messageIDLimit(), "message_id"), Source: c.readAddr(r, c.addressLimit(id), "source_addr"), Destination: c.readAddr(r, c.addressLimit(id), "destination_addr")}
	case pdu.ReplaceSMID:
		v := &pdu.ReplaceSM{MessageID: r.C(c.messageIDLimit(), "message_id"), Source: c.readAddr(r, c.addressLimit(id), "source_addr"), ScheduleDeliveryTime: r.C(17, "schedule_delivery_time"), ValidityPeriod: r.C(17, "validity_period"), RegisteredDelivery: r.U8("registered_delivery"), DefaultMessageID: r.U8("sm_default_msg_id")}
		n := r.U8("sm_length")
		v.ShortMessage = r.Bytes(int(n), "short_message")
		max := c.shortMessageLimit()
		if int(n) > max {
			r.Fail("short_message", "exceeds version bound")
		}
		if r.Err == nil {
			r.Err = c.validateReplacement(v)
		}
		body = v
	case pdu.DataSMID:
		v := &pdu.DataSM{ServiceType: r.C(6, "service_type"), Source: c.readAddr(r, c.addressLimit(id), "source_addr"), Destination: c.readAddr(r, c.addressLimit(id), "destination_addr"), ESMClass: r.U8("esm_class"), RegisteredDelivery: r.U8("registered_delivery"), DataCoding: r.U8("data_coding")}
		v.Optional = c.readTLVs(r, id)
		if r.Err == nil {
			r.Err = c.validateMessageFlags(id, v.ESMClass, v.RegisteredDelivery, v.DataCoding)
		}
		if r.Err == nil {
			r.Err = validateUDHI(v.ESMClass, v.Optional)
		}
		body = v
	case pdu.OutbindID:
		body = &pdu.Outbind{SystemID: r.C(16, "system_id"), Password: r.C(9, "password")}
	case pdu.AlertNotificationID:
		v := &pdu.AlertNotification{Source: c.readAddr(r, c.addressLimit(id), "source_addr"), ESME: c.readAddr(r, c.addressLimit(id), "esme_addr")}
		v.Optional = c.readTLVs(r, id)
		body = v
	default:
		return nil, ErrUnsupported
	}
	if e := r.End(); e != nil {
		return nil, e
	}
	return body, nil
}
func (c *Codec) encodeBody(w *wire.Writer, body pdu.Body) {
	id := body.CommandID()
	switch b := body.(type) {
	case *pdu.BindReceiver:
		c.writeBind(w, b.BindFields)
	case *pdu.BindReceiverResp:
		w.C(b.SystemID, 16, "system_id")
		c.writeTLVs(w, b.Optional, id)
	case *pdu.BindTransmitter:
		c.writeBind(w, b.BindFields)
	case *pdu.BindTransmitterResp:
		w.C(b.SystemID, 16, "system_id")
		c.writeTLVs(w, b.Optional, id)
	case *pdu.BindTransceiver:
		c.writeBind(w, b.BindFields)
	case *pdu.BindTransceiverResp:
		w.C(b.SystemID, 16, "system_id")
		c.writeTLVs(w, b.Optional, id)
	case *pdu.SubmitMulti:
		c.writeMulti(w, b)
	case *pdu.SubmitMultiResp:
		c.writeMultiResp(w, b)
	case *pdu.SubmitSM:
		c.writeSM(w, b.SMFields, id)
	case *pdu.DeliverSM:
		c.writeSM(w, b.SMFields, id)
	case *pdu.SubmitSMResp:
		w.C(b.MessageID, c.messageIDLimit(), "message_id")
		c.writeTLVs(w, b.Optional, id)
	case *pdu.DeliverSMResp:
		w.C(b.MessageID, c.messageIDLimit(), "message_id")
		c.writeTLVs(w, b.Optional, id)
	case *pdu.DataSMResp:
		w.C(b.MessageID, c.messageIDLimit(), "message_id")
		c.writeTLVs(w, b.Optional, id)
	case *pdu.Unbind:
	case *pdu.UnbindResp:
		c.writeTLVs(w, b.Optional, id)
	case *pdu.EnquireLink:
	case *pdu.EnquireLinkResp:
		c.writeTLVs(w, b.Optional, id)
	case *pdu.ReplaceSMResp:
		c.writeTLVs(w, b.Optional, id)
	case *pdu.CancelSMResp:
		c.writeTLVs(w, b.Optional, id)
	case *pdu.GenericNACK:
		c.writeTLVs(w, b.Optional, id)
	case *pdu.QuerySM:
		w.C(b.MessageID, c.messageIDLimit(), "message_id")
		c.writeAddr(w, b.Source, c.addressLimit(id), "source_addr")
	case *pdu.QuerySMResp:
		w.C(b.MessageID, c.messageIDLimit(), "message_id")
		w.C(b.FinalDate, 17, "final_date")
		w.U8(b.MessageState)
		w.U8(b.ErrorCode)
		c.writeTLVs(w, b.Optional, id)
	case *pdu.CancelSM:
		w.C(b.ServiceType, 6, "service_type")
		w.C(b.MessageID, c.messageIDLimit(), "message_id")
		c.writeAddr(w, b.Source, c.addressLimit(id), "source_addr")
		c.writeAddr(w, b.Destination, c.addressLimit(id), "destination_addr")
	case *pdu.ReplaceSM:
		if err := c.validateReplacement(b); err != nil {
			w.Err = err
			return
		}
		max := c.shortMessageLimit()
		if len(b.ShortMessage) > max {
			w.Err = fmt.Errorf("%w: short_message bound", ErrValidation)
			return
		}
		w.C(b.MessageID, c.messageIDLimit(), "message_id")
		c.writeAddr(w, b.Source, c.addressLimit(id), "source_addr")
		w.C(b.ScheduleDeliveryTime, 17, "schedule_delivery_time")
		w.C(b.ValidityPeriod, 17, "validity_period")
		w.U8(b.RegisteredDelivery)
		w.U8(b.DefaultMessageID)
		w.U8(byte(len(b.ShortMessage)))
		w.Bytes(b.ShortMessage)
	case *pdu.DataSM:
		if err := c.validateMessageFlags(id, b.ESMClass, b.RegisteredDelivery, b.DataCoding); err != nil {
			w.Err = err
			return
		}
		if err := validateUDHI(b.ESMClass, b.Optional); err != nil {
			w.Err = err
			return
		}
		w.C(b.ServiceType, 6, "service_type")
		c.writeAddr(w, b.Source, c.addressLimit(id), "source_addr")
		c.writeAddr(w, b.Destination, c.addressLimit(id), "destination_addr")
		w.U8(b.ESMClass)
		w.U8(b.RegisteredDelivery)
		w.U8(b.DataCoding)
		c.writeTLVs(w, b.Optional, id)
	case *pdu.Outbind:
		w.C(b.SystemID, 16, "system_id")
		w.C(b.Password, 9, "password")
	case *pdu.AlertNotification:
		c.writeAddr(w, b.Source, c.addressLimit(id), "source_addr")
		c.writeAddr(w, b.ESME, c.addressLimit(id), "esme_addr")
		c.writeTLVs(w, b.Optional, id)
	default:
		w.Err = fmt.Errorf("%w: expected concrete standard body", ErrValidation)
	}
}
