package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"

	"github.com/OcupointInc/rf-control/client"
	pb "github.com/OcupointInc/rf-control/controlpb"
)

func cmdCustomerCW(args []string) error {
	common, cfg, err := parseCustomerCW(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printCustomerCWUsage()
			return nil
		}
		return err
	}
	tx, err := common.makeTransport()
	if err != nil {
		return err
	}
	c := client.New(tx)
	defer c.Close()
	result, err := c.ConfigureBarracudaCW(cfg)
	if err != nil {
		return err
	}
	printCustomerConfiguration(result)
	return nil
}

func cmdCustomerSweep(args []string) error {
	common, cfg, err := parseCustomerSweep(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printCustomerSweepUsage()
			return nil
		}
		return err
	}
	tx, err := common.makeTransport()
	if err != nil {
		return err
	}
	c := client.New(tx)
	defer c.Close()
	result, err := c.ConfigureBarracudaSweep(cfg)
	if err != nil {
		return err
	}
	printCustomerConfiguration(result)
	return nil
}

func parseCustomerCW(args []string) (*commonFlags, client.BarracudaCWConfig, error) {
	fs := flag.NewFlagSet("cw", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	common := &commonFlags{}
	addCommonFlags(fs, common)
	frequency := fs.Int("frequency", 0, "CW IF frequency in MHz (50..1500)")
	attenuation := fs.Float64("attenuation", 0, "output attenuation in dB (0..31.75, 0.25 dB steps)")
	clock := fs.String("clock", "internal", "clock source: internal or external")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, client.BarracudaCWConfig{}, flag.ErrHelp
		}
		return nil, client.BarracudaCWConfig{}, usagef("cw: %v", err)
	}
	if fs.NArg() != 0 {
		return nil, client.BarracudaCWConfig{}, usagef("usage: cw --frequency MHz [--attenuation dB] [--clock internal|external]")
	}
	if *frequency == 0 {
		return nil, client.BarracudaCWConfig{}, usagef("cw requires --frequency MHz")
	}
	if *frequency < int(client.BarracudaMinIFFrequencyMHz) || *frequency > int(client.BarracudaMaxIFFrequencyMHz) {
		return nil, client.BarracudaCWConfig{}, usagef("CW IF frequency must be %d..%d MHz",
			client.BarracudaMinIFFrequencyMHz, client.BarracudaMaxIFFrequencyMHz)
	}
	external, err := parseCustomerClock(*clock)
	if err != nil {
		return nil, client.BarracudaCWConfig{}, err
	}
	cfg := client.BarracudaCWConfig{
		IFFrequencyMHz: int32(*frequency), AttenuationDB: *attenuation, ExternalClock: external,
	}
	if err := validateCustomerCW(cfg); err != nil {
		return nil, client.BarracudaCWConfig{}, usagef("%v", err)
	}
	return common, cfg, nil
}

func parseCustomerSweep(args []string) (*commonFlags, client.BarracudaSweepConfig, error) {
	fs := flag.NewFlagSet("sweep", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	common := &commonFlags{}
	addCommonFlags(fs, common)
	start := fs.Int("start", 0, "sweep start IF in MHz (50..1500)")
	stop := fs.Int("stop", 0, "sweep stop IF in MHz (50..1500)")
	timeText := fs.String("time", "", "sweep time with units, for example 10s or 35us")
	attenuation := fs.Float64("attenuation", 0, "output attenuation in dB (0..31.75, 0.25 dB steps)")
	clock := fs.String("clock", "internal", "clock source: internal or external")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, client.BarracudaSweepConfig{}, flag.ErrHelp
		}
		return nil, client.BarracudaSweepConfig{}, usagef("sweep: %v", err)
	}
	if fs.NArg() != 0 {
		return nil, client.BarracudaSweepConfig{}, usagef("usage: sweep --start MHz --stop MHz --time DURATION [--attenuation dB] [--clock internal|external]")
	}
	if *start == 0 || *stop == 0 || *timeText == "" {
		return nil, client.BarracudaSweepConfig{}, usagef("sweep requires --start, --stop, and --time")
	}
	if *start < int(client.BarracudaMinIFFrequencyMHz) || *start > int(client.BarracudaMaxIFFrequencyMHz) {
		return nil, client.BarracudaSweepConfig{}, usagef("sweep IF start must be %d..%d MHz",
			client.BarracudaMinIFFrequencyMHz, client.BarracudaMaxIFFrequencyMHz)
	}
	if *stop < int(client.BarracudaMinIFFrequencyMHz) || *stop > int(client.BarracudaMaxIFFrequencyMHz) {
		return nil, client.BarracudaSweepConfig{}, usagef("sweep IF stop must be %d..%d MHz",
			client.BarracudaMinIFFrequencyMHz, client.BarracudaMaxIFFrequencyMHz)
	}
	duration, err := time.ParseDuration(*timeText)
	if err != nil {
		return nil, client.BarracudaSweepConfig{}, usagef("invalid --time %q: include a unit such as 10s, 20ms, or 35us", *timeText)
	}
	external, err := parseCustomerClock(*clock)
	if err != nil {
		return nil, client.BarracudaSweepConfig{}, err
	}
	cfg := client.BarracudaSweepConfig{
		StartIFMHz: int32(*start), StopIFMHz: int32(*stop), SweepTime: duration,
		AttenuationDB: *attenuation, ExternalClock: external,
	}
	if err := validateCustomerSweep(cfg); err != nil {
		return nil, client.BarracudaSweepConfig{}, usagef("%v", err)
	}
	return common, cfg, nil
}

func parseCustomerClock(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "internal", "int":
		return false, nil
	case "external", "ext":
		return true, nil
	default:
		return false, usagef("invalid --clock %q: use internal or external", value)
	}
}

func (cfg *barracudaBatch) customerConfigs() (*client.BarracudaCWConfig, *client.BarracudaSweepConfig, error) {
	clock := cfg.Clock
	if clock == "" {
		clock = "internal"
	}
	external, err := parseCustomerClock(clock)
	if err != nil {
		return nil, nil, err
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Mode)) {
	case "cw":
		if cfg.IFFrequencyMHz == nil {
			return nil, nil, fmt.Errorf("CW mode requires if_frequency_mhz")
		}
		if *cfg.IFFrequencyMHz < int(client.BarracudaMinIFFrequencyMHz) || *cfg.IFFrequencyMHz > int(client.BarracudaMaxIFFrequencyMHz) {
			return nil, nil, fmt.Errorf("CW IF frequency must be %d..%d MHz",
				client.BarracudaMinIFFrequencyMHz, client.BarracudaMaxIFFrequencyMHz)
		}
		if cfg.StartIFMHz != nil || cfg.StopIFMHz != nil || cfg.SweepTime != "" {
			return nil, nil, fmt.Errorf("CW mode does not accept start_if_mhz, stop_if_mhz, or sweep_time")
		}
		cw := &client.BarracudaCWConfig{
			IFFrequencyMHz: int32(*cfg.IFFrequencyMHz), AttenuationDB: cfg.AttenuationDB, ExternalClock: external,
		}
		if err := validateCustomerCW(*cw); err != nil {
			return nil, nil, err
		}
		return cw, nil, nil
	case "sweep":
		if cfg.IFFrequencyMHz != nil {
			return nil, nil, fmt.Errorf("sweep mode does not accept if_frequency_mhz")
		}
		if cfg.StartIFMHz == nil || cfg.StopIFMHz == nil || cfg.SweepTime == "" {
			return nil, nil, fmt.Errorf("sweep mode requires start_if_mhz, stop_if_mhz, and sweep_time")
		}
		if *cfg.StartIFMHz < int(client.BarracudaMinIFFrequencyMHz) || *cfg.StartIFMHz > int(client.BarracudaMaxIFFrequencyMHz) ||
			*cfg.StopIFMHz < int(client.BarracudaMinIFFrequencyMHz) || *cfg.StopIFMHz > int(client.BarracudaMaxIFFrequencyMHz) {
			return nil, nil, fmt.Errorf("sweep IF frequencies must be %d..%d MHz",
				client.BarracudaMinIFFrequencyMHz, client.BarracudaMaxIFFrequencyMHz)
		}
		duration, err := time.ParseDuration(cfg.SweepTime)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid sweep_time %q: include a unit such as 10s, 20ms, or 35us", cfg.SweepTime)
		}
		sweep := &client.BarracudaSweepConfig{
			StartIFMHz: int32(*cfg.StartIFMHz), StopIFMHz: int32(*cfg.StopIFMHz), SweepTime: duration,
			AttenuationDB: cfg.AttenuationDB, ExternalClock: external,
		}
		if err := validateCustomerSweep(*sweep); err != nil {
			return nil, nil, err
		}
		return nil, sweep, nil
	default:
		return nil, nil, fmt.Errorf("mode must be cw or sweep")
	}
}

func validateCustomerCW(cfg client.BarracudaCWConfig) error {
	if cfg.IFFrequencyMHz < client.BarracudaMinIFFrequencyMHz || cfg.IFFrequencyMHz > client.BarracudaMaxIFFrequencyMHz {
		return fmt.Errorf("CW IF frequency must be %d..%d MHz", client.BarracudaMinIFFrequencyMHz, client.BarracudaMaxIFFrequencyMHz)
	}
	return validateCustomerAttenuation(cfg.AttenuationDB)
}

func validateCustomerSweep(cfg client.BarracudaSweepConfig) error {
	if cfg.StartIFMHz < client.BarracudaMinIFFrequencyMHz || cfg.StartIFMHz > client.BarracudaMaxIFFrequencyMHz {
		return fmt.Errorf("sweep IF start must be %d..%d MHz", client.BarracudaMinIFFrequencyMHz, client.BarracudaMaxIFFrequencyMHz)
	}
	if cfg.StopIFMHz < client.BarracudaMinIFFrequencyMHz || cfg.StopIFMHz > client.BarracudaMaxIFFrequencyMHz {
		return fmt.Errorf("sweep IF stop must be %d..%d MHz", client.BarracudaMinIFFrequencyMHz, client.BarracudaMaxIFFrequencyMHz)
	}
	if cfg.StopIFMHz <= cfg.StartIFMHz {
		return fmt.Errorf("sweep stop must be greater than start")
	}
	if cfg.SweepTime <= 0 || cfg.SweepTime%time.Microsecond != 0 {
		return fmt.Errorf("sweep time must be positive and resolve to whole microseconds")
	}
	if cfg.SweepTime/time.Microsecond > time.Duration(math.MaxUint32) {
		return fmt.Errorf("sweep time is too long")
	}
	return validateCustomerAttenuation(cfg.AttenuationDB)
}

func validateCustomerAttenuation(value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > client.BarracudaMaxAttenuationDB {
		return fmt.Errorf("attenuation must be 0..%.2f dB", client.BarracudaMaxAttenuationDB)
	}
	quarterDB := math.Round(value*4) / 4
	if math.Abs(value-quarterDB) > 1e-9 {
		return fmt.Errorf("attenuation must use 0.25 dB steps")
	}
	return nil
}

func printCustomerConfiguration(result *client.BarracudaConfiguration) {
	if result.Mode == "cw" {
		fmt.Printf("CW configured: %d MHz IF\n", result.StartIFMHz)
	} else {
		fmt.Printf("Sweep configured: %d to %d MHz IF in %s\n", result.StartIFMHz, result.StopIFMHz, result.SweepTime)
	}
	clock := "internal"
	if result.ExternalClock {
		clock = "external"
	}
	fmt.Printf("Clock: %s\n", clock)
	fmt.Printf("Attenuation: %.2f dB\n", result.AttenuationDB)
	fmt.Printf("Nominal output: %.2f dBm\n", result.NominalOutputDBm)
	fmt.Printf("Signal locked: %v\n", result.SignalLocked)
}

func printCustomerCWUsage() {
	fmt.Fprintln(os.Stderr, `Usage:
  rf-control [--ip ADDRESS | --usb DEVICE] cw --frequency MHz
             [--attenuation dB] [--clock internal|external]

Frequency is the customer IF and must be 50..1500 MHz.
Attenuation defaults to 0 dB and accepts 0..31.75 dB in 0.25 dB steps.
At 0 dB attenuation the calibrated nominal output is -25 dBm.`)
}

func printCustomerSweepUsage() {
	fmt.Fprintln(os.Stderr, `Usage:
  rf-control [--ip ADDRESS | --usb DEVICE] sweep --start MHz --stop MHz
             --time DURATION [--attenuation dB]
             [--clock internal|external]

Start and stop are customer IF frequencies from 50..1500 MHz; stop must exceed
start. DURATION requires units, for example 10s, 20ms, or 35us.
Attenuation defaults to 0 dB; the calibrated nominal output is -25 dBm.`)
}

func printBarracudaCustomerStatus(status *pb.GetStatusResponse) {
	details := status.GetBarracuda()
	const customerLOHz = uint64(client.BarracudaFixedLOMHz) * 1_000_000
	customerPlan := details != nil && details.GetLmxRequestedFrequencyHz() == customerLOHz &&
		details.GetLmxOutputPowerCode() == client.BarracudaCalibratedLMXPowerCode
	clock := "internal"
	if status.GetClockSourceExternal() {
		clock = "external"
	}
	fmt.Printf("Clock               : %s\n", clock)
	if status.GetClockSourceExternal() {
		fmt.Printf("Reference locked    : %v\n", status.GetRefLocked())
	}
	fmt.Printf("Signal locked       : %v\n", status.GetPllLocked())
	fmt.Printf("Attenuation         : %d dB (whole-dB readback)\n", status.GetAttenuationDb())
	if customerPlan {
		fmt.Printf("Nominal output      : %.2f dBm\n", client.BarracudaNominalOutputDBm-float64(status.GetAttenuationDb()))
	} else {
		fmt.Println("Nominal output      : unavailable (run cw or sweep)")
	}
	if status.GetMcuTemperatureC() != 0 {
		note := ""
		if status.GetMcuTemperatureIsBootSample() {
			note = " (boot sample)"
		}
		fmt.Printf("Controller temp     : %.1f C%s\n", status.GetMcuTemperatureC(), note)
	}
	if details == nil {
		fmt.Println("Barracuda details   : unavailable (older firmware)")
		return
	}
	if adf := details.GetAdfState(); adf != nil {
		mode := "CW"
		if adf.GetRampEnabled() {
			mode = "continuous sweep"
		}
		fmt.Printf("Mode                : %s\n", mode)
		ifFrequencyMHz := adf.GetFrequencyMhz() - client.BarracudaFixedLOMHz
		if customerPlan && ifFrequencyMHz >= client.BarracudaMinIFFrequencyMHz && ifFrequencyMHz <= client.BarracudaMaxIFFrequencyMHz {
			label := "IF frequency"
			if adf.GetRampEnabled() {
				label = "IF start"
			}
			fmt.Printf("%-20s: %d MHz\n", label, ifFrequencyMHz)
		}
	}
}

func printBarracudaEngineeringStatus(status *pb.GetStatusResponse) {
	details := status.GetBarracuda()
	if details == nil {
		return
	}
	fmt.Println("--- Engineering synthesizer details ---")
	if adf := details.GetAdfState(); adf != nil {
		fmt.Printf("ADF start/frequency : %d MHz\n", adf.GetFrequencyMhz())
	}
	fmt.Printf("LO requested/actual : %.6f / %.6f MHz\n",
		float64(details.GetLmxRequestedFrequencyHz())/1e6,
		float64(details.GetLmxActualFrequencyHz())/1e6)
	fmt.Printf("LO locked           : %v\n", details.GetLmxLocked())
	fmt.Printf("LMX power code      : %d (manual=%v)\n",
		details.GetLmxOutputPowerCode(), details.GetLmxOutputPowerManual())
	if caps := details.GetCapabilities(); caps != nil {
		fmt.Printf("ADF safe range      : %d..%d MHz\n",
			client.BarracudaFixedLOMHz+client.BarracudaMinIFFrequencyMHz, caps.GetAdfMaxFrequencyMhz())
	}
}
