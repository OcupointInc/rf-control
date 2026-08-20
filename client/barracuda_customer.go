package client

import (
	"fmt"
	"math"
	"time"

	pb "github.com/OcupointInc/rf-control/controlpb"
)

const (
	// BarracudaFixedLOMHz is the customer operating plan's fixed LMX2595 LO.
	BarracudaFixedLOMHz int32 = 9600
	// BarracudaNominalOutputDBm is the measured nominal output at 0 dB DSA.
	BarracudaNominalOutputDBm = -25.0
	// BarracudaCalibratedLMXPowerCode is the LMX2595 OUTA_PWR setting used
	// when the nominal -25 dBm output level was established.
	BarracudaCalibratedLMXPowerCode uint32 = 50
	BarracudaMinIFFrequencyMHz      int32  = 50
	BarracudaMaxIFFrequencyMHz      int32  = 1500
	BarracudaMaxAttenuationDB              = 31.75
)

// BarracudaCWConfig is the small customer-facing configuration for a CW IF.
// The RF synthesizer plan is derived internally and is not customer-settable.
type BarracudaCWConfig struct {
	IFFrequencyMHz int32
	AttenuationDB  float64
	ExternalClock  bool
}

// BarracudaSweepConfig is the customer-facing configuration for a continuous
// sawtooth IF sweep. The RF synthesizer plan is derived internally.
type BarracudaSweepConfig struct {
	StartIFMHz    int32
	StopIFMHz     int32
	SweepTime     time.Duration
	AttenuationDB float64
	ExternalClock bool
}

// BarracudaConfiguration describes the state requested by ConfigureBarracudaCW
// or ConfigureBarracudaSweep. NominalOutputDBm is the calibrated nominal level:
// -25 dBm at 0 dB attenuation, reduced one dB per dB of attenuation.
type BarracudaConfiguration struct {
	Mode             string
	StartIFMHz       int32
	StopIFMHz        int32
	SweepTime        time.Duration
	AttenuationDB    float64
	NominalOutputDBm float64
	ExternalClock    bool
	SignalLocked     bool
}

// SetBarracudaClockSource selects the Barracuda LMK clock source and returns
// the live reference-selection and lock result. external=false uses the onboard
// oscillator; external=true uses the external 10 MHz PRIREF/DPLL path.
func (c *Client) SetBarracudaClockSource(external bool) (*pb.SetClockSourceResponse, error) {
	resp, err := c.send(&pb.Packet{MessageId: &pb.Packet_SetClockSourceRequest{
		SetClockSourceRequest: &pb.SetClockSourceRequest{External: external},
	}})
	if err != nil {
		return nil, err
	}
	got, ok := resp.MessageId.(*pb.Packet_SetClockSourceResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type: %T", resp.MessageId)
	}
	return got.SetClockSourceResponse, nil
}

// SetLMXOutputPower selects the LMX2595 RFOUTA OUTA_PWR code (0..63).
func (c *Client) SetLMXOutputPower(powerCode uint32) error {
	if powerCode > 63 {
		return fmt.Errorf("LMX2595 output power code must be 0..63")
	}
	resp, err := c.send(&pb.Packet{MessageId: &pb.Packet_SetLmxOutputPowerRequest{
		SetLmxOutputPowerRequest: &pb.SetLmxOutputPowerRequest{PowerCode: powerCode},
	}})
	if err != nil {
		return err
	}
	if _, ok := resp.MessageId.(*pb.Packet_SetLmxOutputPowerResponse); !ok {
		return fmt.Errorf("unexpected response type: %T", resp.MessageId)
	}
	return nil
}

// SetLMKClockOutputs sets all four Barracuda LMK-routed outputs and returns the
// live channel-active mask.
func (c *Client) SetLMKClockOutputs(req *pb.SetLmkClockOutputsRequest) (*pb.SetLmkClockOutputsResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("LMK clock-output request is nil")
	}
	resp, err := c.send(&pb.Packet{MessageId: &pb.Packet_SetLmkClockOutputsRequest{SetLmkClockOutputsRequest: req}})
	if err != nil {
		return nil, err
	}
	got, ok := resp.MessageId.(*pb.Packet_SetLmkClockOutputsResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type: %T", resp.MessageId)
	}
	return got.SetLmkClockOutputsResponse, nil
}

// SetLMKReferenceFrequency requests a coordinated Barracuda reference-clock
// change and returns the actual realizable frequency plus downstream settings.
func (c *Client) SetLMKReferenceFrequency(frequencyHz uint32) (*pb.SetLmkReferenceFrequencyResponse, error) {
	resp, err := c.send(&pb.Packet{MessageId: &pb.Packet_SetLmkReferenceFrequencyRequest{
		SetLmkReferenceFrequencyRequest: &pb.SetLmkReferenceFrequencyRequest{RequestedFrequencyHz: frequencyHz},
	}})
	if err != nil {
		return nil, err
	}
	got, ok := resp.MessageId.(*pb.Packet_SetLmkReferenceFrequencyResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type: %T", resp.MessageId)
	}
	return got.SetLmkReferenceFrequencyResponse, nil
}

// ReadLMX2595Registers reads the requested LMX2595 register addresses.
func (c *Client) ReadLMX2595Registers(addresses []uint32) (*pb.Lmx2595RegisterReadResponse, error) {
	resp, err := c.send(&pb.Packet{MessageId: &pb.Packet_Lmx2595RegisterReadRequest{
		Lmx2595RegisterReadRequest: &pb.Lmx2595RegisterReadRequest{Addresses: addresses},
	}})
	if err != nil {
		return nil, err
	}
	got, ok := resp.MessageId.(*pb.Packet_Lmx2595RegisterReadResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type: %T", resp.MessageId)
	}
	if !got.Lmx2595RegisterReadResponse.GetSuccess() {
		return nil, fmt.Errorf("LMX2595 register read failed")
	}
	return got.Lmx2595RegisterReadResponse, nil
}

// RunEqualizedSweep runs the firmware's one-shot, DSA-equalized sweep.
func (c *Client) RunEqualizedSweep(req *pb.RunEqualizedSweepRequest) (*pb.RunEqualizedSweepResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("equalized sweep request is nil")
	}
	resp, err := c.send(&pb.Packet{MessageId: &pb.Packet_RunEqualizedSweepRequest{RunEqualizedSweepRequest: req}})
	if err != nil {
		return nil, err
	}
	got, ok := resp.MessageId.(*pb.Packet_RunEqualizedSweepResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type: %T", resp.MessageId)
	}
	return got.RunEqualizedSweepResponse, nil
}

// ConfigureBarracudaCW safely applies the complete customer CW plan. It verifies
// the board, mutes the DSA during reconfiguration, derives the internal RF plan
// from the requested IF, then applies the requested attenuation.
func (c *Client) ConfigureBarracudaCW(cfg BarracudaCWConfig) (*BarracudaConfiguration, error) {
	quarterDB, err := validateBarracudaAttenuation(cfg.AttenuationDB)
	if err != nil {
		return nil, err
	}
	if err := validateBarracudaIFFrequency("CW IF frequency", cfg.IFFrequencyMHz); err != nil {
		return nil, err
	}
	if err := c.prepareBarracudaCustomerPlan(cfg.ExternalClock); err != nil {
		return nil, err
	}
	if err := c.SetPllFrequency(barracudaRFFrequencyMHz(cfg.IFFrequencyMHz)); err != nil {
		return nil, fmt.Errorf("set CW IF frequency: %w", err)
	}
	status, err := c.GetStatus()
	if err != nil {
		return nil, fmt.Errorf("verify CW configuration: %w", err)
	}
	if err := verifyBarracudaLO(status); err != nil {
		return nil, err
	}
	if !status.GetPllLocked() {
		return nil, &DeviceError{Code: pb.ErrorCode_ERROR_CODE_HARDWARE_ERROR, Detail: "signal generator did not lock; output remains muted"}
	}
	if err := c.SetDsaAttenuation(quarterDB); err != nil {
		return nil, fmt.Errorf("set attenuation: %w", err)
	}
	return &BarracudaConfiguration{
		Mode: "cw", StartIFMHz: cfg.IFFrequencyMHz, StopIFMHz: cfg.IFFrequencyMHz,
		AttenuationDB:    cfg.AttenuationDB,
		NominalOutputDBm: BarracudaNominalOutputDBm - cfg.AttenuationDB,
		ExternalClock:    cfg.ExternalClock, SignalLocked: status.GetPllLocked(),
	}, nil
}

// ConfigureBarracudaSweep safely applies a continuous sawtooth sweep using the
// customer operating plan. Only IF start, IF stop, duration, attenuation, and
// clock source are configurable; the RF synthesizer plan remains internal.
func (c *Client) ConfigureBarracudaSweep(cfg BarracudaSweepConfig) (*BarracudaConfiguration, error) {
	quarterDB, err := validateBarracudaAttenuation(cfg.AttenuationDB)
	if err != nil {
		return nil, err
	}
	if err := validateBarracudaIFFrequency("sweep IF start", cfg.StartIFMHz); err != nil {
		return nil, err
	}
	if err := validateBarracudaIFFrequency("sweep IF stop", cfg.StopIFMHz); err != nil {
		return nil, err
	}
	if cfg.StopIFMHz <= cfg.StartIFMHz {
		return nil, fmt.Errorf("sweep stop must be greater than start")
	}
	rampTimeUs, err := barracudaSweepMicroseconds(cfg.SweepTime)
	if err != nil {
		return nil, err
	}
	if err := c.prepareBarracudaCustomerPlan(cfg.ExternalClock); err != nil {
		return nil, err
	}
	locked, err := c.SetChirp(
		barracudaRFFrequencyMHz(cfg.StartIFMHz), cfg.StopIFMHz-cfg.StartIFMHz, rampTimeUs,
		pb.ChirpMode_CHIRP_MODE_SAWTOOTH_CONTINUOUS, true,
	)
	if err != nil {
		return nil, fmt.Errorf("set sweep: %w", err)
	}
	if !locked {
		return nil, &DeviceError{Code: pb.ErrorCode_ERROR_CODE_HARDWARE_ERROR, Detail: "signal generator did not lock; output remains muted"}
	}
	status, err := c.GetStatus()
	if err != nil {
		return nil, fmt.Errorf("verify sweep configuration: %w", err)
	}
	if err := verifyBarracudaLO(status); err != nil {
		return nil, err
	}
	if err := c.SetDsaAttenuation(quarterDB); err != nil {
		return nil, fmt.Errorf("set attenuation: %w", err)
	}
	return &BarracudaConfiguration{
		Mode: "sweep", StartIFMHz: cfg.StartIFMHz, StopIFMHz: cfg.StopIFMHz, SweepTime: cfg.SweepTime,
		AttenuationDB:    cfg.AttenuationDB,
		NominalOutputDBm: BarracudaNominalOutputDBm - cfg.AttenuationDB,
		ExternalClock:    cfg.ExternalClock, SignalLocked: locked,
	}, nil
}

func (c *Client) prepareBarracudaCustomerPlan(externalClock bool) error {
	status, err := c.GetStatus()
	if err != nil {
		return fmt.Errorf("identify device: %w", err)
	}
	if status.GetBoardType() != "barracuda" {
		return &DeviceError{Code: pb.ErrorCode_ERROR_CODE_UNSUPPORTED,
			Detail: fmt.Sprintf("customer CW/sweep control requires a Barracuda device (got %q)", status.GetBoardType())}
	}
	// Mute first. Any later failure leaves the RF path at maximum attenuation.
	if err := c.SetDsaAttenuation(127); err != nil {
		return fmt.Errorf("mute output before reconfiguration: %w", err)
	}
	clock, err := c.SetBarracudaClockSource(externalClock)
	if err != nil {
		return fmt.Errorf("set clock source: %w", err)
	}
	if clock.GetExternal() != externalClock {
		return &DeviceError{Code: pb.ErrorCode_ERROR_CODE_HARDWARE_ERROR,
			Detail: "clock-source readback did not match the requested source; output remains muted"}
	}
	if externalClock && (!clock.GetReferenceValid() || !clock.GetReferenceSelected() ||
		!clock.GetDpllFrequencyLocked() || !clock.GetDpllPhaseLocked()) {
		return &DeviceError{Code: pb.ErrorCode_ERROR_CODE_HARDWARE_ERROR,
			Detail: "external reference was selected but is not valid and fully locked; output remains muted"}
	}
	if err := c.SetLoFrequency(BarracudaFixedLOMHz); err != nil {
		return fmt.Errorf("configure internal frequency plan: %w", err)
	}
	// Force the calibrated baseline rather than inheriting a manual power code
	// left behind by an earlier engineering session.
	if err := c.SetLMXOutputPower(BarracudaCalibratedLMXPowerCode); err != nil {
		return fmt.Errorf("configure calibrated output level: %w", err)
	}
	return nil
}

func verifyBarracudaLO(status *pb.GetStatusResponse) error {
	details := status.GetBarracuda()
	if details == nil {
		return &DeviceError{Code: pb.ErrorCode_ERROR_CODE_UNSUPPORTED,
			Detail: "firmware does not provide internal frequency verification; output remains muted"}
	}
	const expectedHz = uint64(BarracudaFixedLOMHz) * 1_000_000
	if details.GetLmxRequestedFrequencyHz() != expectedHz || !details.GetLmxLocked() {
		return &DeviceError{Code: pb.ErrorCode_ERROR_CODE_HARDWARE_ERROR,
			Detail: "internal frequency plan could not be verified; output remains muted"}
	}
	if details.GetLmxOutputPowerCode() != BarracudaCalibratedLMXPowerCode {
		return &DeviceError{Code: pb.ErrorCode_ERROR_CODE_HARDWARE_ERROR,
			Detail: "internal output power is not at the calibrated customer setting; output remains muted"}
	}
	return nil
}

func validateBarracudaIFFrequency(name string, frequencyMHz int32) error {
	if frequencyMHz < BarracudaMinIFFrequencyMHz || frequencyMHz > BarracudaMaxIFFrequencyMHz {
		return fmt.Errorf("%s must be %d..%d MHz", name, BarracudaMinIFFrequencyMHz, BarracudaMaxIFFrequencyMHz)
	}
	return nil
}

func barracudaRFFrequencyMHz(ifFrequencyMHz int32) int32 {
	return BarracudaFixedLOMHz + ifFrequencyMHz
}

func validateBarracudaAttenuation(attenuationDB float64) (int32, error) {
	if math.IsNaN(attenuationDB) || math.IsInf(attenuationDB, 0) ||
		attenuationDB < 0 || attenuationDB > BarracudaMaxAttenuationDB {
		return 0, fmt.Errorf("attenuation must be 0..%.2f dB", BarracudaMaxAttenuationDB)
	}
	quarterDB := math.Round(attenuationDB * 4)
	if math.Abs(attenuationDB-quarterDB/4) > 1e-9 {
		return 0, fmt.Errorf("attenuation must use 0.25 dB steps")
	}
	return int32(quarterDB), nil
}

func barracudaSweepMicroseconds(duration time.Duration) (uint32, error) {
	if duration <= 0 {
		return 0, fmt.Errorf("sweep time must be positive")
	}
	if duration%time.Microsecond != 0 {
		return 0, fmt.Errorf("sweep time must resolve to a whole number of microseconds")
	}
	microseconds := duration / time.Microsecond
	if microseconds > math.MaxUint32 {
		return 0, fmt.Errorf("sweep time must not exceed %s", time.Duration(math.MaxUint32)*time.Microsecond)
	}
	return uint32(microseconds), nil
}
