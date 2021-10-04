package charts

import (
	"embed"
)

//go:embed consul/Chart.yaml consul/values.yaml consul/templates consul/templates/_helpers.tpl
var ConsulHelmChart embed.FS
