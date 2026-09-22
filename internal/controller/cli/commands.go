package cli

import (
	"context"
	"flag"
	"fmt"
	"sort"
	"strings"

	"orca/internal/extensions/engine"
	"orca/internal/modules/discovery"
	"orca/internal/modules/layers"
	"orca/internal/modules/storage"
	"orca/internal/modules/volumes"
)

func infoCmd(_ *flag.FlagSet) func(context.Context, *env) error {
	return func(ctx context.Context, e *env) error {
		info, err := e.svc.Discovery.Daemon(ctx)
		if err != nil {
			return err
		}
		if e.json {
			return writeJSON(e.stdout, info)
		}

		w := newTable(e.stdout)
		fmt.Fprintf(w, "Host:\t%s\n", info.Host)
		fmt.Fprintf(w, "Server version:\t%s\n", info.ServerVersion)
		fmt.Fprintf(w, "API version:\t%s (daemon max %s)\n", info.APIVersion, info.MaxAPIVersion)
		fmt.Fprintf(w, "Operating system:\t%s (%s/%s)\n", info.OS, info.OSType, info.Architecture)
		fmt.Fprintf(w, "Kernel:\t%s\n", info.KernelVersion)
		fmt.Fprintf(w, "CPUs / memory:\t%d / %s\n", info.CPUs, size(info.MemoryBytes))
		fmt.Fprintf(w, "Storage driver:\t%s\n", info.StorageDriver)
		fmt.Fprintf(w, "Data root:\t%s\n", info.DataRoot)
		fmt.Fprintf(w, "Logging driver:\t%s\n", info.LoggingDriver)
		fmt.Fprintf(w, "Containers:\t%d (running %d, paused %d, stopped %d)\n",
			info.Containers, info.Running, info.Paused, info.Stopped)
		fmt.Fprintf(w, "Images:\t%d\n", info.Images)
		return w.Flush()
	}
}

func containersCmd(fs *flag.FlagSet) func(context.Context, *env) error {
	running := fs.Bool("running", false, "only running, paused and restarting containers")
	stopped := fs.Bool("stopped", false, "only stopped containers (created, exited, dead)")
	withSize := fs.Bool("size", false, "compute writable-layer sizes (slow)")

	return func(ctx context.Context, e *env) error {
		state := discovery.AllContainers
		switch {
		case *running && *stopped:
			return usageError{"--running and --stopped are mutually exclusive"}
		case *running:
			state = discovery.RunningContainers
		case *stopped:
			state = discovery.StoppedContainers
		}

		cs, err := e.svc.Discovery.Containers(ctx, state, *withSize)
		if err != nil {
			return err
		}
		if e.json {
			return writeJSON(e.stdout, cs)
		}

		w := newTable(e.stdout)
		fmt.Fprintln(w, "CONTAINER ID\tNAME\tIMAGE\tIMAGE ID\tSTATE\tCREATED\tSIZE\tNETWORKS\tMOUNTS")
		for _, c := range cs {
			sz := "-"
			if c.SizeRw >= 0 {
				sz = fmt.Sprintf("%s (virtual %s)", size(c.SizeRw), size(c.SizeRootFs))
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				shortID(c.ID), c.Name, c.ImageRef, shortID(c.ImageID), c.State, ago(c.CreatedAt),
				sz, orDash(strings.Join(c.Networks, ",")), mounts(c.Mounts))
		}
		if err := w.Flush(); err != nil {
			return err
		}
		fmt.Fprintf(e.stderr, "%d containers\n", len(cs))
		return nil
	}
}

func mounts(ms []engine.Mount) string {
	parts := make([]string, 0, len(ms))
	for _, m := range ms {
		src := m.Source
		if m.Type == engine.MountVolume {
			src = m.Name
		}
		parts = append(parts, fmt.Sprintf("%s:%s->%s", m.Type, src, m.Destination))
	}
	return orDash(strings.Join(parts, ","))
}

func imagesCmd(fs *flag.FlagSet) func(context.Context, *env) error {
	dangling := fs.Bool("dangling", false, "only dangling images (untagged, no child image)")
	untagged := fs.Bool("untagged", false, "only untagged images (dangling and intermediate)")
	unused := fs.Bool("unused", false, "only images no container was created from")
	used := fs.Bool("used", false, "only images at least one container was created from")

	return func(ctx context.Context, e *env) error {
		if *used && *unused {
			return usageError{"--used and --unused are mutually exclusive"}
		}

		all, err := e.svc.Discovery.Images(ctx)
		if err != nil {
			return err
		}

		var imgs []discovery.Image
		for _, img := range all {
			switch {
			case *dangling && img.Kind != discovery.Dangling,
				*untagged && !img.Untagged(),
				*unused && img.Referenced(),
				*used && !img.Referenced():
				continue
			}
			imgs = append(imgs, img)
		}
		sort.SliceStable(imgs, func(i, j int) bool { return imgs[i].CreatedAt.After(imgs[j].CreatedAt) })

		if e.json {
			return writeJSON(e.stdout, imgs)
		}

		w := newTable(e.stdout)
		fmt.Fprintln(w, "REPOSITORY:TAG\tIMAGE ID\tKIND\tPARENT\tCREATED\tSIZE\tCONTAINERS\tDIGEST")
		for _, img := range imgs {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%d\t%s\n",
				imageName(img.Image), shortID(img.ID), img.Kind, orDash(shortID(img.ParentID)),
				ago(img.CreatedAt), size(img.Size), len(img.Containers), orDash(firstDigest(img.RepoDigests)))
		}
		if err := w.Flush(); err != nil {
			return err
		}

		counts := map[discovery.ImageKind]int{}
		referenced := 0
		for _, img := range imgs {
			counts[img.Kind]++
			if img.Referenced() {
				referenced++
			}
		}
		fmt.Fprintf(e.stderr, "%d images: %d tagged, %d dangling, %d intermediate; %d used by containers, %d unused\n",
			len(imgs), counts[discovery.Tagged], counts[discovery.Dangling], counts[discovery.Intermediate],
			referenced, len(imgs)-referenced)
		return nil
	}
}

func volumesCmd(_ *flag.FlagSet) func(context.Context, *env) error {
	return func(ctx context.Context, e *env) error {
		inv, err := e.svc.Inventory.Collect(ctx)
		if err != nil {
			return err
		}
		printWarnings(e, inv.Warnings)
		if e.json {
			return writeJSON(e.stdout, inv.Volumes)
		}

		w := newTable(e.stdout)
		fmt.Fprintln(w, "NAME\tKIND\tSTATE\tSIZE\tINODES\tCONTAINERS\tPROJECT\tLAST USED\tREASON")
		counts := map[volumes.State]int{}
		for _, v := range inv.Volumes {
			counts[v.State]++
			name := v.Name
			if v.Kind == volumes.Anonymous {
				name = shortID(name)
			}
			inodes := "-"
			if v.Inodes >= 0 {
				inodes = fmt.Sprint(v.Inodes)
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\t%s\t%s\t%s\n",
				name, v.Kind, v.State, sizeOrDash(v.Size), inodes, len(v.Containers),
				orDash(v.Project.Name), ago(v.LastUsed), strings.Join(v.Reasons, "; "))
		}
		if err := w.Flush(); err != nil {
			return err
		}
		fmt.Fprintf(e.stderr, "%d volumes: %d referenced, %d protected, %d candidates, %d need review\n",
			len(inv.Volumes), counts[volumes.Referenced], counts[volumes.Protected],
			counts[volumes.Candidate], counts[volumes.Unreferenced])
		return nil
	}
}

func networksCmd(_ *flag.FlagSet) func(context.Context, *env) error {
	return func(ctx context.Context, e *env) error {
		ns, err := e.svc.Discovery.Networks(ctx)
		if err != nil {
			return err
		}
		if e.json {
			return writeJSON(e.stdout, ns)
		}

		w := newTable(e.stdout)
		fmt.Fprintln(w, "NETWORK ID\tNAME\tDRIVER\tSCOPE\tINTERNAL")
		for _, n := range ns {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%t\n", shortID(n.ID), n.Name, n.Driver, n.Scope, n.Internal)
		}
		if err := w.Flush(); err != nil {
			return err
		}
		fmt.Fprintf(e.stderr, "%d networks\n", len(ns))
		return nil
	}
}

func layersCmd(fs *flag.FlagSet) func(context.Context, *env) error {
	imageRef := fs.String("image", "", "show the layers of one image (`ref`: name:tag or ID prefix)")
	all := fs.Bool("all", false, "list every layer, not only shared ones")

	return func(ctx context.Context, e *env) error {
		snap, err := e.svc.Layers.Snapshot(ctx)
		if err != nil {
			return err
		}
		names := make(map[string]string, len(snap.Images))
		for _, img := range snap.Images {
			names[img.ID] = imageName(img)
		}

		if *imageRef != "" {
			img, err := discovery.ResolveImage(snap.Images, *imageRef)
			if err != nil {
				return err
			}
			return imageLayers(e, snap.Index, img, names)
		}

		ls := snap.Index.Shared()
		if *all {
			ls = snap.Index.All()
		}
		dups := snap.Index.DuplicatedDiffIDs()

		if e.json {
			return writeJSON(e.stdout, struct {
				ImageLayers       map[string][]string
				Layers            []*layers.Layer
				DuplicatedDiffIDs map[string][]string
			}{snap.Index.ImageLayers, ls, dups})
		}

		w := newTable(e.stdout)
		fmt.Fprintln(w, "CHAIN ID\tDIFF ID\tOVERLAY2 DIR\tDEPTH\tREFS\tIMAGES")
		for _, l := range ls {
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%s\n", shortID(l.ChainID), shortID(l.DiffID),
				dirList(l.StorageDirs), l.Depth, l.RefCount(), imageList(l.Images, names))
		}
		if err := w.Flush(); err != nil {
			return err
		}

		fmt.Fprintf(e.stderr, "%d images, %d unique layers, %d shared by more than one image\n",
			len(snap.Images), len(snap.Index.Layers), len(snap.Index.Shared()))
		fmt.Fprintf(e.stderr, "%d DiffIDs stored under more than one ChainID, %d layers stored in more than one overlay2 directory\n",
			len(dups), len(snap.Index.MultiDir()))
		return nil
	}
}

func imageLayers(e *env, idx layers.Index, img engine.Image, names map[string]string) error {
	chain := idx.ImageLayers[img.ID]
	if e.json {
		ls := make([]*layers.Layer, 0, len(chain))
		for _, id := range chain {
			ls = append(ls, idx.Layers[id])
		}
		return writeJSON(e.stdout, struct {
			ImageID string
			Layers  []*layers.Layer
		}{img.ID, ls})
	}

	w := newTable(e.stdout)
	fmt.Fprintln(w, "#\tDIFF ID\tCHAIN ID\tOVERLAY2 DIR\tREFS\tSHARED WITH")
	for i, id := range chain {
		l := idx.Layers[id]
		var others []string
		for _, other := range l.Images {
			if other != img.ID {
				others = append(others, other)
			}
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%d\t%s\n", i+1, shortID(l.DiffID), shortID(l.ChainID),
			dirList(l.StorageDirs), l.RefCount(), imageList(others, names))
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(e.stderr, "%s (%s): %d layers, bottom first\n", imageName(img), shortID(img.ID), len(chain))
	return nil
}

func storageCmd(fs *flag.FlagSet) func(context.Context, *env) error {
	noMeasure := fs.Bool("no-measure", false, "skip walking the overlay2 directory")

	return func(ctx context.Context, e *env) error {
		r, err := e.svc.Storage.Inspect(ctx, !*noMeasure)
		if err != nil {
			return err
		}
		diff, ratio, measured := r.Discrepancy()

		if e.json {
			out := struct {
				storage.Report
				APITotal         int64
				Discrepancy      *int64   `json:",omitempty"`
				DiscrepancyRatio *float64 `json:",omitempty"`
			}{Report: r, APITotal: r.APITotal()}
			if measured {
				out.Discrepancy, out.DiscrepancyRatio = &diff, &ratio
			}
			return writeJSON(e.stdout, out)
		}

		w := newTable(e.stdout)
		fmt.Fprintf(w, "Storage driver:\t%s\n", r.Driver)
		for _, kv := range r.DriverStatus {
			fmt.Fprintf(w, "  %s:\t%s\n", kv[0], kv[1])
		}
		fmt.Fprintf(w, "Data root:\t%s\n", r.DataRoot)
		switch {
		case r.ContainerdSnapshotter:
			fmt.Fprintf(w, "Image store:\tcontainerd snapshotter (layers are not in %s)\n", r.Overlay2Dir)
		case r.Overlay2:
			fmt.Fprintf(w, "Image store:\tclassic overlay2 graph driver (layers in %s)\n", r.Overlay2Dir)
		default:
			fmt.Fprintf(w, "Image store:\tgraph driver %s\n", r.Driver)
		}

		fmt.Fprintln(w, "API-reported usage:\t")
		fmt.Fprintf(w, "  Image layers:\t%s\n", size(r.API.ImagesSize))
		fmt.Fprintf(w, "  Container writable layers:\t%s\n", size(r.API.ContainersSize))
		fmt.Fprintf(w, "  Build cache:\t%s\n", size(r.API.BuildCacheSize))
		fmt.Fprintf(w, "  Total:\t%s\n", size(r.APITotal()))

		if !measured {
			fmt.Fprintf(w, "overlay2 on disk:\tnot measured: %s\n", r.MeasureSkipped)
			return w.Flush()
		}
		m := r.Measured
		fmt.Fprintf(w, "overlay2 on disk:\t%s\n", m.Path)
		fmt.Fprintf(w, "  Apparent size:\t%s\n", size(m.ApparentBytes))
		fmt.Fprintf(w, "  Allocated on disk:\t%s\n", size(m.AllocatedBytes))
		fmt.Fprintf(w, "  Files / directories:\t%d / %d\n", m.Files, m.Dirs)
		if m.Unreadable > 0 {
			fmt.Fprintf(w, "  Unreadable entries:\t%d (sizes are a lower bound)\n", m.Unreadable)
		}
		verdict := "within tolerance"
		if ratio > storage.DiscrepancyThreshold || ratio < -storage.DiscrepancyThreshold {
			verdict = "significant, see docs/storage-model.md"
		}
		sign := "+"
		if diff < 0 {
			sign = "-"
			diff = -diff
		}
		fmt.Fprintf(w, "Measured vs API:\t%s%s (%+.1f%%, %s)\n", sign, size(diff), ratio*100, verdict)
		return w.Flush()
	}
}
