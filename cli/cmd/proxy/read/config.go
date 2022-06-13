package read

type Config struct {
	Clusters  []Cluster
	Endpoints []Endpoint
	Listeners []Listener
	Routes    []Route
	Secrets   []Secret
}

type Cluster struct{}

type Endpoint struct{}

type Listener struct{}

type Route struct{}

type Secret struct{}

func ParseConfig(config []byte) (Config, error) {
	return Config{}, nil
}
