package docker

import "time"

// These types mirror the subset of the Docker Engine API's JSON responses
// that Back-Orbit needs. See
// https://docs.docker.com/reference/api/engine/ for the authoritative
// schema. Keeping a minimal, hand-written wire format (rather than
// depending on the full docker/docker SDK's type packages) keeps the
// dependency graph small — see the docker client.go doc comment.

type containerSummary struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Image   string            `json:"Image"`
	ImageID string            `json:"ImageID"`
	Created int64             `json:"Created"`
	State   string            `json:"State"`
	Status  string            `json:"Status"`
	Labels  map[string]string `json:"Labels"`
	Mounts  []mountSummary    `json:"Mounts"`
}

type mountSummary struct {
	Type        string `json:"Type"`
	Name        string `json:"Name"`
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
	RW          bool   `json:"RW"`
}

func (cs containerSummary) toContainer() Container {
	name := cs.ID
	if len(cs.Names) > 0 {
		name = trimLeadingSlash(cs.Names[0])
	}

	mounts := make([]Mount, 0, len(cs.Mounts))
	for _, m := range cs.Mounts {
		mounts = append(mounts, Mount{
			Type:        toMountType(m.Type),
			Name:        m.Name,
			Source:      m.Source,
			Destination: m.Destination,
			ReadOnly:    !m.RW,
		})
	}

	return Container{
		ID:        cs.ID,
		Name:      name,
		Service:   cs.Labels[LabelService],
		Image:     cs.Image,
		ImageID:   cs.ImageID,
		State:     cs.State,
		Status:    cs.Status,
		CreatedAt: time.Unix(cs.Created, 0).UTC(),
		Labels:    cs.Labels,
		Mounts:    mounts,
	}
}

func trimLeadingSlash(s string) string {
	if len(s) > 0 && s[0] == '/' {
		return s[1:]
	}
	return s
}

func toMountType(t string) MountType {
	switch MountType(t) {
	case MountTypeVolume, MountTypeBind, MountTypeTmpfs:
		return MountType(t)
	default:
		return MountTypeOther
	}
}

type volumeSummary struct {
	Name       string            `json:"Name"`
	Driver     string            `json:"Driver"`
	Mountpoint string            `json:"Mountpoint"`
	Labels     map[string]string `json:"Labels"`
}

type volumeListResponse struct {
	Volumes  []volumeSummary `json:"Volumes"`
	Warnings []string        `json:"Warnings"`
}

type networkSummary struct {
	ID     string            `json:"Id"`
	Name   string            `json:"Name"`
	Driver string            `json:"Driver"`
	Labels map[string]string `json:"Labels"`
}
