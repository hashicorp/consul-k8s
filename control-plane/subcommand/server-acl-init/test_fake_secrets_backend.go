package serveraclinit

type FakeSecretsBackend struct {
	bootstrapToken string
}

func (b *FakeSecretsBackend) BootstrapToken() (string, error) {
	return b.bootstrapToken, nil
}

func (*FakeSecretsBackend) BootstrapTokenSecretName() string {
	return "fake-bootstrap-token"
}

func (b *FakeSecretsBackend) WriteBootstrapToken(token string) error {
	b.bootstrapToken = token
	return nil
}

var _ SecretsBackend = (*FakeSecretsBackend)(nil)
