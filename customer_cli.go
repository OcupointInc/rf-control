package main

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
	frequency := fs.Int("frequency", 0, "CW frequency in MHz (9500..11500)")
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
	if *frequency < int(client.BarracudaMinFrequencyMHz) || *frequency > int(client.BarracudaMaxFrequencyMHz) {
		return nil, client.BarracudaCWConfig{}, usagef("CW frequency must be %d..%d MHz",
			client.BarracudaMinFrequencyMHz, client.BarracudaMaxFrequencyMHz)
	}
	external, err := parseCustomerClock(*clock)
	if err != nil {
		return nil, client.BarracudaCWConfig{}, err
	}
	cfg := client.BarracudaCWConfig{
		FrequencyMHz: int32(*frequency), AttenuationDB: *attenuation, ExternalClock: external,
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
	start := fs.Int("start", 0, "sweep start frequency in MHz (9500..11500)")
	stop := fs.Int("stop", 0, "sweep stop frequency in MHz (9500..11500)")
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
	if *start < int(client.BarracudaMinFrequencyMHz) || *start > int(client.BarracudaMaxFrequencyMHz) {
		return nil, client.BarracudaSweepConfig{}, usagef("sweep start must be %d..%d MHz",
			client.BarracudaMinFrequencyMHz, client.BarracudaMaxFrequencyMHz)
	}
	if *stop < int(client.BarracudaMinFrequencyMHz) || *stop > int(client.BarracudaMaxFrequencyMHz) {
		return nil, client.BarracudaSweepConfig{}, usagef("sweep stop must be %d..%d MHz",
			client.BarracudaMinFrequencyMHz, client.BarracudaMaxFrequencyMHz)
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
		StartMHz: int32(*start), StopMHz: int32(*stop), SweepTime: duration,
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
		if cfg.FrequencyMHz == nil {
			return nil, nil, fmt.Errorf("CW mode requires frequency_mhz")
		}
		if *cfg.FrequencyMHz < int(client.BarracudaMinFrequencyMHz) || *cfg.FrequencyMHz > int(client.BarracudaMaxFrequencyMHz) {
			return nil, nil, fmt.Errorf("CW frequency must be %d..%d MHz",
				client.BarracudaMinFrequencyMHz, client.BarracudaMaxFrequencyMHz)
		}
		if cfg.StartMHz != nil || cfg.StopMHz != nil || cfg.SweepTime != "" {
			return nil, nil, fmt.Errorf("CW mode does not accept start_mhz, stop_mhz, or sweep_time")
		}
		cw := &client.BarracudaCWConfig{
			FrequencyMHz: int32(*cfg.FrequencyMHz), AttenuationDB: cfg.AttenuationDB, ExternalClock: external,
		}
		if err := validateCustomerCW(*cw); err != nil {
			return nil, nil, err
		}
		return cw, nil, nil
	case "sweep":
		if cfg.FrequencyMHz != nil {
			return nil, nil, fmt.Errorf("sweep mode does not accept frequency_mhz")
		}
		if cfg.StartMHz == nil || cfg.StopMHz == nil || cfg.SweepTime == "" {
			return nil, nil, fmt.Errorf("sweep mode requires start_mhz, stop_mhz, and sweep_time")
		}
		if *cfg.StartMHz < int(client.BarracudaMinFrequencyMHz) || *cfg.StartMHz > int(client.BarracudaMaxFrequencyMHz) ||
			*cfg.StopMHz < int(client.BarracudaMinFrequencyMHz) || *cfg.StopMHz > int(client.BarracudaMaxFrequencyMHz) {
			return nil, nil, fmt.Errorf("sweep frequencies must be %d..%d MHz",
				client.BarracudaMinFrequencyMHz, client.BarracudaMaxFrequencyMHz)
		}
		duration, err := time.ParseDuration(cfg.SweepTime)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid sweep_time %q: include a unit such as 10s, 20ms, or 35us", cfg.SweepTime)
		}
		sweep := &client.BarracudaSweepConfig{
			StartMHz: int32(*cfg.StartMHz), StopMHz: int32(*cfg.StopMHz), SweepTime: duration,
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
	if cfg.FrequencyMHz < client.BarracudaMinFrequencyMHz || cfg.FrequencyMHz > client.BarracudaMaxFrequencyMHz {
		return fmt.Errorf("CW frequency must be %d..%d MHz", client.BarracudaMinFrequencyMHz, client.BarracudaMaxFrequencyMHz)
	}
	return validateCustomerAttenuation(cfg.AttenuationDB)
}

func validateCustomerSweep(cfg client.BarracudaSweepConfig) error {
	if cfg.StartMHz < client.BarracudaMinFrequencyMHz || cfg.StartMHz > client.BarracudaMaxFrequencyMHz {
		return fmt.Errorf("sweep start must be %d..%d MHz", client.BarracudaMinFrequencyMHz, client.BarracudaMaxFrequencyMHz)
	}
	if cfg.StopMHz < client.BarracudaMinFrequencyMHz || cfg.StopMHz > client.BarracudaMaxFrequencyMHz {
		return fmt.Errorf("sweep stop must be %d..%d MHz", client.BarracudaMinFrequencyMHz, client.BarracudaMaxFrequencyMHz)
	}
	if cfg.StopMHz <= cfg.StartMHz {
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
		fmt.Printf("CW configured: %d MHz\n", result.StartMHz)
	} else {
		fmt.Printf("Sweep configured: %d to %d MHz in %s\n", result.StartMHz, result.StopMHz, result.SweepTime)
	}
	clock := "internal"
	if result.ExternalClock {
		clock = "external"
	}
	fmt.Printf("LO: %d MHz (fixed)\n", result.LOFrequencyMHz)
	fmt.Printf("Clock: %s\n", clock)
	fmt.Printf("Attenuation: %.2f dB\n", result.AttenuationDB)
	fmt.Printf("Nominal output: %.2f dBm\n", result.NominalOutputDBm)
	fmt.Printf("ADF lock: %v\n", result.ADFLocked)
}

func printCustomerCWUsage() {
	fmt.Fprintln(os.Stderr, `Usage:
  rf-control [--ip ADDRESS | --usb DEVICE] cw --frequency MHz
             [--attenuation dB] [--clock internal|external]

The LO is fixed at 9600 MHz. Frequency must be 9500..11500 MHz.
Attenuation defaults to 0 dB and accepts 0..31.75 dB in 0.25 dB steps.
At 0 dB attenuation the calibrated nominal output is -25 dBm.`)
}

func printCustomerSweepUsage() {
	fmt.Fprintln(os.Stderr, `Usage:
  rf-control [--ip ADDRESS | --usb DEVICE] sweep --start MHz --stop MHz
             --time DURATION [--attenuation dB]
             [--clock internal|external]

The LO is fixed at 9600 MHz. Frequencies must be 9500..11500 MHz and stop
must exceed start. DURATION requires units, for example 10s, 20ms, or 35us.
Attenuation defaults to 0 dB; the calibrated nominal output is -25 dBm.`)
}

func printBarracudaCustomerStatus(status *pb.GetStatusResponse) {
	clock := "internal"
	if status.GetClockSourceExternal() {
		clock = "external"
	}
	fmt.Printf("Clock               : %s\n", clock)
	fmt.Printf("Clock locked        : %v\n", status.GetRefLocked())
	fmt.Printf("ADF locked          : %v\n", status.GetPllLocked())
	fmt.Printf("Attenuation         : %d dB (whole-dB readback)\n", status.GetAttenuationDb())
	fmt.Printf("Nominal output      : %.2f dBm\n", client.BarracudaNominalOutputDBm-float64(status.GetAttenuationDb()))
	if status.GetMcuTemperatureC() != 0 {
		note := ""
		if status.GetMcuTemperatureIsBootSample() {
			note = " (boot sample)"
		}
		fmt.Printf("Controller temp     : %.1f C%s\n", status.GetMcuTemperatureC(), note)
	}
	details := status.GetBarracuda()
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
			client.BarracudaMinFrequencyMHz, caps.GetAdfMaxFrequencyMhz())
	}
}
