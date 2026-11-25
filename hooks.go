// Copyright 2026 High Five Labs
// SPDX-License-Identifier: Apache-2.0

package extensions

import (
	"context"
	"errors"

	"github.com/highfivelabs/krm-rest-runtime/descriptor"
	"github.com/highfivelabs/krm-rest-runtime/reconciler"
	"github.com/highfivelabs/krm-rest-runtime/rest"
)

// Hooks change request bodies before they are sent, by kind.
var Hooks = map[string][]reconciler.Hook{
	"Schedule": {ScheduleLayerIDs},
}

// ScheduleLayerIDs gives each schedule layer in an update the ID PagerDuty
// gave it. A PUT /schedules/{id} whose layers carry no ID adds them as new
// layers beside the old ones, so every update would grow the schedule (the
// live run found three layers after one update). A spec layer is matched to
// the layer read back with the same name, or else by position: PagerDuty
// reads layers back highest priority first, the reverse of how they are sent.
func ScheduleLayerIDs(ctx context.Context, c *rest.Client, k *descriptor.Kind, params map[string]string, body map[string]any) error {
	layers, _ := body["schedule_layers"].([]any)
	if params[k.ID.Param] == "" || len(layers) == 0 {
		return nil // a create: PagerDuty assigns the IDs
	}
	path, err := rest.Expand(k.Read.Path, params)
	if err != nil {
		return err
	}
	resp, err := c.Do(ctx, k.Read.Method, path, nil)
	if errors.Is(err, rest.ErrNotFound) {
		return nil // gone: the reconciler creates it again
	}
	if err != nil {
		return err
	}
	cur, _ := descriptor.Dig(resp, k.Envelope+"/schedule_layers").([]any)
	byName := map[string]string{}
	for _, l := range cur {
		m, _ := l.(map[string]any)
		if name, _ := m["name"].(string); name != "" {
			byName[name] = idOf(m)
		}
	}
	for i, l := range layers {
		m, ok := l.(map[string]any)
		if !ok || m["id"] != nil {
			continue
		}
		id := ""
		if name, _ := m["name"].(string); name != "" {
			id = byName[name]
		} else if len(cur) == len(layers) {
			id = idOf(cur[len(cur)-1-i])
		}
		if id != "" {
			m["id"] = id
		}
	}
	return nil
}

func idOf(v any) string {
	m, _ := v.(map[string]any)
	id, _ := m["id"].(string)
	return id
}
