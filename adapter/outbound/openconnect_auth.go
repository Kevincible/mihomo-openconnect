package outbound

import (
	"context"
	"errors"

	oc "github.com/sagernet/sing-openconnect"
)

var ErrOpenConnectAuthenticationRequired = errors.New("openconnect requires interactive authentication; configure credentials or a host authentication handler")

type OpenConnectAuthField struct {
	Key     string
	Name    string
	Label   string
	Kind    string
	Value   string
	Choices map[string]string
}

type OpenConnectAuthChallenge struct {
	Message string
	Error   string
	Fields  []OpenConnectAuthField
}

// OpenConnectAuthHandler supplies form values from the host's credential store or UI.
// Implementations must return promptly when ctx is canceled. Values are never logged.
type OpenConnectAuthHandler interface {
	AuthenticateOpenConnect(ctx context.Context, challenge OpenConnectAuthChallenge) (map[string]string, error)
}

func (o *OpenConnect) authenticate() {
	defer o.workers.Done()
	for {
		updated := o.client.AuthChallengeUpdated()
		challenge := o.client.PendingAuthChallenge()
		if challenge != nil {
			if o.option.AuthHandler == nil || challenge.Form == nil {
				o.stop(ErrOpenConnectAuthenticationRequired)
				return
			}
			request := OpenConnectAuthChallenge{Message: challenge.Message, Error: challenge.Error}
			for _, field := range challenge.Form.Fields {
				f := OpenConnectAuthField{Key: field.SubmissionKey, Name: field.Name, Label: field.Label, Kind: field.Kind, Value: field.Value}
				if len(field.Options) > 0 {
					f.Choices = make(map[string]string, len(field.Options))
					for _, choice := range field.Options {
						f.Choices[choice.Value] = choice.Label
					}
				}
				request.Fields = append(request.Fields, f)
			}
			values, err := o.option.AuthHandler.AuthenticateOpenConnect(o.ctx, request)
			if err != nil {
				o.stop(ErrOpenConnectAuthenticationRequired)
				return
			}
			err = o.client.CompleteAuthChallenge(challenge.ID, oc.AuthResponse{Form: &oc.AuthFormResponse{Values: values}})
			if err != nil && !errors.Is(err, oc.ErrNoPendingAuthChallenge) {
				o.stop(err)
				return
			}
			continue
		}
		select {
		case <-o.ctx.Done():
			return
		case <-updated:
		}
	}
}
