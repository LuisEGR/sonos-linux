package sonos

import "fmt"

// Player controls a Sonos device via UPnP/SOAP.
type Player struct {
	Device *Device
}

func NewPlayer(dev *Device) *Player {
	return &Player{Device: dev}
}

func (p *Player) coordinatorIP() string {
	return p.Device.IP
}

func (p *Player) SetAVTransportURI(uri string) error {
	_, err := soapCall(p.coordinatorIP(),
		"/MediaRenderer/AVTransport/Control",
		"AVTransport",
		"SetAVTransportURI",
		[]soapArg{
			{"InstanceID", "0"},
			{"CurrentURI", uri},
			{"CurrentURIMetaData", ""},
		},
	)
	return err
}

func (p *Player) Play() error {
	_, err := soapCall(p.coordinatorIP(),
		"/MediaRenderer/AVTransport/Control",
		"AVTransport",
		"Play",
		[]soapArg{
			{"InstanceID", "0"},
			{"Speed", "1"},
		},
	)
	return err
}

func (p *Player) Stop() error {
	_, err := soapCall(p.coordinatorIP(),
		"/MediaRenderer/AVTransport/Control",
		"AVTransport",
		"Stop",
		[]soapArg{
			{"InstanceID", "0"},
		},
	)
	return err
}

func (p *Player) Pause() error {
	_, err := soapCall(p.coordinatorIP(),
		"/MediaRenderer/AVTransport/Control",
		"AVTransport",
		"Pause",
		[]soapArg{
			{"InstanceID", "0"},
		},
	)
	return err
}

func (p *Player) SetVolume(level int) error {
	_, err := soapCall(p.Device.IP,
		"/MediaRenderer/RenderingControl/Control",
		"RenderingControl",
		"SetVolume",
		[]soapArg{
			{"InstanceID", "0"},
			{"Channel", "Master"},
			{"DesiredVolume", fmt.Sprintf("%d", level)},
		},
	)
	return err
}

func (p *Player) GetVolume() (string, error) {
	result, err := soapCall(p.Device.IP,
		"/MediaRenderer/RenderingControl/Control",
		"RenderingControl",
		"GetVolume",
		[]soapArg{
			{"InstanceID", "0"},
			{"Channel", "Master"},
		},
	)
	if err != nil {
		return "", err
	}
	return result["CurrentVolume"], nil
}
