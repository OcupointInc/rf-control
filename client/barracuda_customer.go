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
	BarracudaMinFrequencyMHz        int32  = 9500
	BarracudaMaxFrequencyMHz        int32  = 11500
	BarracudaMaxAttenuationDB              = 31.75
)

// BarracudaCWConfig is the small customer-facing configuration for a CW tone.
// The LMX2595 LO is always set to BarracudaFixedLOMHz.
type BarracudaCWConfig struct {
	FrequencyMHz  int32
	AttenuationDB float64
	ExternalClock bool
}

// BarracudaSweepConfig is the small customer-facing configuration for a
// continuous sawtooth sweep. The LMX2595 LO is always fixed at 9600 MHz.
type BarracudaSweepConfig struct {
	StartMHz      int32
	StopMHz       int32
	SweepTime     time.Duration
	AttenuationDB float64
	ExternalClock bool
}

// BarracudaConfiguration describes the state requested by ConfigureBarracudaCW
// or ConfigureBarracudaSweep. NominalOutputDBm is the calibrated nominal level:
// -25 dBm at 0 dB attenuation, reduced one dB per dB of attenuation.
type BarracudaConfiguration struct {
	Mode             string
	LOFrequencyMHz   int32
	StartMHz         int32
	StopMHz          int32
	SweepTime        time.Duration
	AttenuationDB    float64
	NominalOutputDBm float64
	ExternalClock    bool
	ADFLocked        bool
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
// the board, mutes the DSA during reconfiguration, fixes the LO at 9600 MHz,
// programs the ADF4159 CW tone, then applies the requested attenuation.
func (c *Client) ConfigureBarracudaCW(cfg BarracudaCWConfig) (*BarracudaConfiguration, error) {
	quarterDB, err := validateBarracudaAttenuation(cfg.AttenuationDB)
	if err != nil {
		return nil, err
	}
	if err := validateBarracudaFrequency("CW frequency", cfg.FrequencyMHz); err != nil {
		return nil, err
	}
	if err := c.prepareBarracudaCustomerPlan(cfg.ExternalClock); err != nil {
		return nil, err
	}
	if err := c.SetPllFrequency(cfg.FrequencyMHz); err != nil {
		return nil, fmt.Errorf("set CW frequency: %w", err)
	}
	status, err := c.GetStatus()
	if err != nil {
		return nil, fmt.Errorf("verify CW configuration: %w", err)
	}
	if err := verifyBarracudaLO(status); err != nil {
		return nil, err
	}
	if !status.GetPllLocked() {
		return nil, &DeviceError{Code: pb.ErrorCode_ERROR_CODE_HARDWARE_ERROR, Detail: "ADF4159 did not lock; output remains muted"}
	}
	if err := c.SetDsaAttenuation(quarterDB); err != nil {
		return nil, fmt.Errorf("set attenuation: %w", err)
	}
	return &BarracudaConfiguration{
		Mode: "cw", LOFrequencyMHz: BarracudaFixedLOMHz,
		StartMHz: cfg.FrequencyMHz, StopMHz: cfg.FrequencyMHz,
		AttenuationDB:    cfg.AttenuationDB,
		NominalOutputDBm: BarracudaNominalOutputDBm - cfg.AttenuationDB,
		ExternalClock:    cfg.ExternalClock, ADFLocked: status.GetPllLocked(),
	}, nil
}

// ConfigureBarracudaSweep safely applies a continuous sawtooth sweep using the
// customer operating plan. Only start, stop, duration, attenuation, and clock
// source are configurable; the LO remains fixed at 9600 MHz.
func (c *Client) ConfigureBarracudaSweep(cfg BarracudaSweepConfig) (*BarracudaConfiguration, error) {
	quarterDB, err := validateBarracudaAttenuation(cfg.AttenuationDB)
	if err != nil {
		return nil, err
	}
	if err := validateBarracudaFrequency("sweep start", cfg.StartMHz); err != nil {
		return nil, err
	}
	if err := validateBarracudaFrequency("sweep stop", cfg.StopMHz); err != nil {
		return nil, err
	}
	if cfg.StopMHz <= cfg.StartMHz {
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
		cfg.StartMHz, cfg.StopMHz-cfg.StartMHz, rampTimeUs,
		pb.ChirpMode_CHIRP_MODE_SAWTOOTH_CONTINUOUS, true,
	)
	if err != nil {
		return nil, fmt.Errorf("set sweep: %w", err)
	}
	if !locked {
		return nil, &DeviceError{Code: pb.ErrorCode_ERROR_CODE_HARDWARE_ERROR, Detail: "ADF4159 did not lock; output remains muted"}
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
		Mode: "sweep", LOFrequencyMHz: BarracudaFixedLOMHz,
		StartMHz: cfg.StartMHz, StopMHz: cfg.StopMHz, SweepTime: cfg.SweepTime,
		AttenuationDB:    cfg.AttenuationDB,
		NominalOutputDBm: BarracudaNominalOutputDBm - cfg.AttenuationDB,
		ExternalClock:    cfg.ExternalClock, ADFLocked: locked,
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
		return fmt.Errorf("set fixed 9600 MHz LO: %w", err)
	}
	// Force the calibrated baseline rather than inheriting a manual power code
	// left behind by an earlier engineering session.
	if err := c.SetLMXOutputPower(BarracudaCalibratedLMXPowerCode); err != nil {
		return fmt.Errorf("set calibrated LMX2595 output power: %w", err)
	}
	return nil
}

func verifyBarracudaLO(status *pb.GetStatusResponse) error {
	details := status.GetBarracuda()
	if details == nil {
		return &DeviceError{Code: pb.ErrorCode_ERROR_CODE_UNSUPPORTED,
			Detail: "Barracuda firmware does not provide LO verification; output remains muted"}
	}
	const expectedHz = uint64(BarracudaFixedLOMHz) * 1_000_000
	if details.GetLmxRequestedFrequencyHz() != expectedHz || !details.GetLmxLocked() {
		return &DeviceError{Code: pb.ErrorCode_ERROR_CODE_HARDWARE_ERROR,
			Detail: "LMX2595 is not verified at the fixed 9600 MHz LO; output remains muted"}
	}
	if details.GetLmxOutputPowerCode() != BarracudaCalibratedLMXPowerCode {
		return &DeviceError{Code: pb.ErrorCode_ERROR_CODE_HARDWARE_ERROR,
			Detail: "LMX2595 output power is not at the calibrated customer setting; output remains muted"}
	}
	return nil
}

func validateBarracudaFrequency(name string, frequencyMHz int32) error {
	if frequencyMHz < BarracudaMinFrequencyMHz || frequencyMHz > BarracudaMaxFrequencyMHz {
		return fmt.Errorf("%s must be %d..%d MHz", name, BarracudaMinFrequencyMHz, BarracudaMaxFrequencyMHz)
	}
	return nil
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
