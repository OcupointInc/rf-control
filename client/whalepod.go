package client

// Whalepod is a mutable view of a Whalepod board's RF settings. Set the
// fields you care about, then call Write to push them all to the device in
// one go. It's a convenience layer over Client's individual setters for the
// common "change a few things and apply" workflow — the firmware has no
// single "write everything" request, so Write just sends each setter in
// turn.
//
// Whalepod embeds *Client, so the lower-level calls (GetConfig, GetStatus,
// the individual SetX setters, Close) are all available on it directly.
//
// Write sends every field each time, so a freshly-constructed Whalepod at
// its zero value writes "channels off, 0 dB attenuation, not in calibration
// mode." Either set every field explicitly, or call Read first to load the
// current channel/attenuation/cal-mode state off the device, before Write —
// otherwise you'll clobber settings you meant to leave alone.
type Whalepod struct {
	*Client

	Attenuation       int32 // frontend attenuator, 0-31 dB
	CalAttenuation    int32 // calibration-path attenuator, 0-31 dB
	ChannelsEnabled   bool  // enable all RF channels
	CalEnabled        bool  // enter calibration mode (CAL_SW)
	CalSourceInternal bool  // internal noise source (true) vs external CAL connector (false)
}

// NewWhalepod returns a Whalepod bound to tx. All settings start at their
// zero value; see the type doc about Write clobbering unset fields.
func NewWhalepod(tx Transport) *Whalepod {
	return &Whalepod{Client: New(tx)}
}

// Read loads the settings the firmware reports in its status — channels
// enabled, frontend attenuation, and calibration mode — from the device, so
// you can change a couple of fields and Write without clobbering the rest.
// CalAttenuation and CalSourceInternal are left as-is: the firmware doesn't
// report them back, so set those explicitly if you care about them.
func (w *Whalepod) Read() error {
	s, err := w.GetStatus()
	if err != nil {
		return err
	}
	w.ChannelsEnabled = s.ChannelsEnabled
	w.Attenuation = s.AttenuationDb
	w.CalEnabled = s.CalibrationEnabled
	return nil
}

// Write pushes every field to the device. The calibration source is set
// before calibration mode is toggled, so the internal noise-source amp
// comes up in the right state when entering cal mode.
func (w *Whalepod) Write() error {
	if err := w.SetChannelsEnabled(w.ChannelsEnabled); err != nil {
		return err
	}
	if err := w.SetAttenuation(w.Attenuation); err != nil {
		return err
	}
	if err := w.SetCalAttenuation(w.CalAttenuation); err != nil {
		return err
	}
	if err := w.SetCalSource(w.CalSourceInternal); err != nil {
		return err
	}
	if err := w.SetCalEnabled(w.CalEnabled); err != nil {
		return err
	}
	return nil
}
