// Package client is a Go library for talking to Ocupoint Ethernet-controlled
// RF frontends (Black Canyon, Straps, Whalepod) over the same USB-CDC or TCP
// control channel the rf-control CLI uses. Import it directly when you want
// to drive a device from your own Go program instead of shelling out to the
// rf-control binary — e.g. from a test harness or an automated test bench.
//
// # Transports
//
// Two Transport implementations are provided:
//
//   - NewTCPTransport(host, port) talks to the device's control port
//     (default 5000) over the network.
//   - NewUSBTransport(devicePath) talks to the device's binary control
//     channel on its second USB-CDC interface (see the wire format note
//     below). Use ListCandidatePorts / IsControlPort / DiscoverUSBPort to
//     find the right device path without hardcoding it.
//
// Both retry-safe: TCPTransport retries connection-level errors internally
// (see its doc comment for why that's necessary — the device's control port
// only has two listening sockets). Wrap either one in a Client to get typed
// request/response methods instead of building *controlpb.Packet values by
// hand.
//
// # Example
//
//	tx := client.NewTCPTransport("192.168.1.50", 5000)
//	c := client.New(tx)
//	defer c.Close()
//
//	cfg, err := c.GetConfig()
//	if err != nil {
//		log.Fatal(err)
//	}
//	fmt.Println(cfg.SerialNumber)
//
//	if err := c.SetAttenuation(10); err != nil {
//		log.Fatal(err)
//	}
//
// See examples/whalepod in this repo for a complete, runnable program.
package client

import (
	"fmt"

	pb "github.com/OcupointInc/rf-control/controlpb"
)

// Client wraps a Transport with typed methods for the control protocol
// (control.proto). It has no state of its own beyond the transport, so it's
// cheap to construct and safe to discard/recreate as needed — but like the
// underlying Transport, a Client is not safe for concurrent use by multiple
// goroutines.
type Client struct {
	tx Transport
}

// New wraps tx in a Client. The caller retains ownership of tx and should
// call Client.Close (equivalently tx.Close) when done.
func New(tx Transport) *Client {
	return &Client{tx: tx}
}

// Close closes the underlying transport. For TCPTransport this is a no-op
// (each request already opens and closes its own connection); for
// USBTransport it closes the serial port.
func (c *Client) Close() error {
	return c.tx.Close()
}

func (c *Client) send(p *pb.Packet) (*pb.Packet, error) {
	return c.tx.Send(p)
}

// GetConfig reads the device's persisted network configuration (IP,
// gateway, subnet, hostname, MAC, serial number, firmware version).
func (c *Client) GetConfig() (*pb.GetConfigResponse, error) {
	resp, err := c.send(&pb.Packet{MessageId: &pb.Packet_GetConfigRequest{GetConfigRequest: &pb.GetConfigRequest{}}})
	if err != nil {
		return nil, err
	}
	got, ok := resp.MessageId.(*pb.Packet_GetConfigResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type: %T", resp.MessageId)
	}
	return got.GetConfigResponse, nil
}

// GetStatus reads live RF status: channel/calibration enable state,
// attenuation, and (on boards with an RF frontend — not Whalepod, which has
// only attenuators) LO frequency and switch positions. Check
// GetStatusResponse.BoardType to know which fields are meaningful.
func (c *Client) GetStatus() (*pb.GetStatusResponse, error) {
	resp, err := c.send(&pb.Packet{MessageId: &pb.Packet_GetStatusRequest{GetStatusRequest: &pb.GetStatusRequest{}}})
	if err != nil {
		return nil, err
	}
	got, ok := resp.MessageId.(*pb.Packet_GetStatusResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type: %T", resp.MessageId)
	}
	return got.GetStatusResponse, nil
}

// SaveConfig writes new network configuration to flash and reboots the
// device to apply it. Callers typically start from a GetConfig response so
// fields they don't want to change (MacAddress, SerialNumber, ...) are
// preserved.
//
// The device replies before it starts the flash write + reboot, but on some
// transports (notably USB) the connection can still be torn down before the
// reply is fully delivered. A transport-level error immediately after a
// SaveConfigRequest is therefore expected, not necessarily a real failure —
// SaveConfig treats it as success (nil error) rather than making every
// caller special-case it. If the device was genuinely unreachable, GetConfig
// would have failed already when you read the current config to build req.
func (c *Client) SaveConfig(req *pb.SaveConfigRequest) error {
	resp, err := c.send(&pb.Packet{MessageId: &pb.Packet_SaveConfigRequest{SaveConfigRequest: req}})
	if err != nil {
		return nil // expected: device is rebooting, see doc comment above
	}
	if _, ok := resp.MessageId.(*pb.Packet_SaveConfigResponse); !ok {
		return fmt.Errorf("unexpected response type: %T", resp.MessageId)
	}
	return nil
}

// SetAttenuation sets the frontend attenuator, 0-31 dB.
func (c *Client) SetAttenuation(db int32) error {
	resp, err := c.send(&pb.Packet{MessageId: &pb.Packet_SetFrontendAttenuationRequest{
		SetFrontendAttenuationRequest: &pb.SetAttenuationRequest{AttenuationDb: db},
	}})
	if err != nil {
		return err
	}
	if _, ok := resp.MessageId.(*pb.Packet_SetFrontendAttenuationResponse); !ok {
		return fmt.Errorf("unexpected response type: %T", resp.MessageId)
	}
	return nil
}

// SetCalAttenuation sets the calibration-path attenuator, 0-31 dB.
func (c *Client) SetCalAttenuation(db int32) error {
	resp, err := c.send(&pb.Packet{MessageId: &pb.Packet_SetCalAttenuationRequest{
		SetCalAttenuationRequest: &pb.SetAttenuationRequest{AttenuationDb: db},
	}})
	if err != nil {
		return err
	}
	if _, ok := resp.MessageId.(*pb.Packet_SetCalAttenuationResponse); !ok {
		return fmt.Errorf("unexpected response type: %T", resp.MessageId)
	}
	return nil
}

// SetChannelsEnabled enables or disables all RF channels.
func (c *Client) SetChannelsEnabled(enabled bool) error {
	resp, err := c.send(&pb.Packet{MessageId: &pb.Packet_SetChannelsEnabledRequest{
		SetChannelsEnabledRequest: &pb.SetChannelsEnabledRequest{Enabled: enabled},
	}})
	if err != nil {
		return err
	}
	if _, ok := resp.MessageId.(*pb.Packet_SetChannelsEnabledResponse); !ok {
		return fmt.Errorf("unexpected response type: %T", resp.MessageId)
	}
	return nil
}

// SetCalEnabled enters or leaves calibration mode (CAL_SW). On Whalepod,
// the internal noise-source amplifier only turns on when calibration mode
// is active AND the internal source is selected — see SetCalSource.
func (c *Client) SetCalEnabled(enabled bool) error {
	resp, err := c.send(&pb.Packet{MessageId: &pb.Packet_SetCalEnabledRequest{
		SetCalEnabledRequest: &pb.SetCalibrationEnabledRequest{Enabled: enabled},
	}})
	if err != nil {
		return err
	}
	if _, ok := resp.MessageId.(*pb.Packet_SetCalEnabledResponse); !ok {
		return fmt.Errorf("unexpected response type: %T", resp.MessageId)
	}
	return nil
}

// SetCalSource selects the Whalepod calibration source (CAL_SEL):
// internal = the on-board noise source, external = the CAL SMA connector.
// Boards without a CAL_SEL line (e.g. whalepod_automation) accept this
// request but it's a no-op in firmware.
func (c *Client) SetCalSource(internal bool) error {
	resp, err := c.send(&pb.Packet{MessageId: &pb.Packet_SetCalSourceRequest{
		SetCalSourceRequest: &pb.SetCalSourceRequest{Internal: internal},
	}})
	if err != nil {
		return err
	}
	if _, ok := resp.MessageId.(*pb.Packet_SetCalSourceResponse); !ok {
		return fmt.Errorf("unexpected response type: %T", resp.MessageId)
	}
	return nil
}
