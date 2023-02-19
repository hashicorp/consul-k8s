package helm

import (
	"embed"
	"helm.sh/helm/v3/pkg/chart/loader"
	"io/fs"
	"path"
)

// readAllChartFiles improve `readChartFiles`, this method can read all files including sub paths
func readAllChartFiles(chart embed.FS, scanDirs []string, chartDirName string) ([]*loader.BufferedFile, error) {
	var chartFiles []*loader.BufferedFile
	for _, scanD := range scanDirs {
		dirs, err := chart.ReadDir(path.Join(chartDirName, scanD))
		if err != nil {
			// continue if the path not found
			if _, s := err.(*fs.PathError); s {
				continue
			}
			return nil, err
		}
		for _, f := range dirs {
			if f.IsDir() {
				resultFiles, err := readAllChartFiles(chart, []string{scanD + "/" + f.Name()}, chartDirName)
				if err != nil {
					return nil, err
				}
				chartFiles = append(chartFiles, resultFiles...)
				continue
			}

			file, err := readFile(chart, path.Join(chartDirName, scanD, f.Name()), chartDirName)

			if err != nil {
				return nil, err
			}
			chartFiles = append(chartFiles, file)
		}
	}

	return chartFiles, nil
}
