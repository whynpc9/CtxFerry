package app

import (
	"context"
	"slices"
)

var configurationOrder = [...]string{layerGlobal, layerProject, layerProfile, layerDevice, layerCheckout}

type CheckoutConfigurationImpact struct {
	Project  Project
	Checkout CurrentCheckout
	Before   ProjectConfigurationValue
	After    ProjectConfigurationValue
	Changed  bool
	Unknown  string
}

func localConfigurationLayer(layer string) bool {
	return layer == layerDevice || layer == layerCheckout
}

func projectConfigurationAfter(loc configurationLocation, current []EffectiveValue, change ConfigurationChange, stored any) ProjectConfigurationValue {
	next := withoutConfigurationLayer(current, change.Layer)
	if change.Value != nil {
		storage := "shared"
		if localConfigurationLayer(change.Layer) {
			storage = "local-only"
		}
		next = append(next, EffectiveValue{change.Field, stored, ConfigOrigin{Layer: change.Layer, Storage: storage}})
		slices.SortFunc(next, func(a, b EffectiveValue) int {
			return slices.Index(configurationOrder[:], a.Origin.Layer) - slices.Index(configurationOrder[:], b.Origin.Layer)
		})
	}
	return effectiveProjectValue(loc, next)
}

// Configuration layers on other devices are local to those devices. The
// impact preview resolves the known local, subscribed checkout bindings using
// the same records and precedence as the selected checkout.
func configurationImpact(ctx context.Context, q projectReader, loc configurationLocation, preview ProjectConfigurationPreview) ([]CheckoutConfigurationImpact, error) {
	bindings, err := impactBindings(ctx, q, loc, preview.Change.Layer)
	if err != nil {
		return nil, err
	}
	var impact []CheckoutConfigurationImpact
	for _, binding := range bindings {
		item := CheckoutConfigurationImpact{Project: binding.project, Checkout: *binding.checkout}
		if loc.checkout != nil && loc.checkout.Origin.ID == binding.checkout.Origin.ID {
			item.Before, item.After = preview.Before, preview.After
		} else {
			item.Before, item.After, item.Unknown = checkoutConfigurationImpact(ctx, q, binding, preview)
		}
		item.Changed = !sameConfigurationEffect(item.Before, item.After)
		impact = append(impact, item)
	}
	return impact, nil
}

func checkoutConfigurationImpact(ctx context.Context, q rowQuerier, loc configurationLocation, preview ProjectConfigurationPreview) (before, after ProjectConfigurationValue, unknown string) {
	profile, err := selectedProfile(ctx, q, loc)
	if err != nil {
		return before, after, Classify(err).Code
	}
	values, err := projectConfigurationValues(ctx, q, loc, profile)
	if err != nil {
		return before, after, Classify(err).Code
	}
	change := preview.Change
	current := values[change.Field]
	before = effectiveProjectValue(loc, current)
	if change.Layer == layerProfile && (profile == nil || profile.Name != change.Profile) {
		return before, before, ""
	}
	return before, projectConfigurationAfter(loc, current, change, preview.Stored), ""
}

func sameConfigurationEffect(a, b ProjectConfigurationValue) bool {
	if a.Effective == nil || b.Effective == nil {
		return a.Effective == b.Effective
	}
	return a.Problem == b.Problem && a.Effective.Value == b.Effective.Value &&
		a.Effective.Origin.Layer == b.Effective.Origin.Layer
}

func impactBindings(ctx context.Context, q projectReader, loc configurationLocation, layer string) ([]configurationLocation, error) {
	path := ""
	if loc.checkout != nil {
		path = loc.checkout.Path
	}
	rows, err := q.QueryContext(ctx, `SELECT p.id,p.slug,p.created_at,b.id,b.path
		FROM checkout_bindings b JOIN projects p ON p.id=b.project_id
		WHERE b.device_id=? AND (?='global' OR p.id=?) AND (?<>'checkout' OR b.path=?)
		AND `+live("p.id")+`
		AND NOT EXISTS (SELECT 1 FROM project_subscriptions s WHERE s.project_id=p.id AND s.subscribed=0)
		ORDER BY p.slug,p.id,b.path`, loc.device, layer, loc.project.ID, layer, path)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var bindings []configurationLocation
	for rows.Next() {
		project := Project{}
		checkout := CurrentCheckout{Origin: ConfigOrigin{Layer: layerCheckout, Source: "checkout-binding", Storage: "local-only"}}
		if err := rows.Scan(&project.ID, &project.Slug, &project.CreatedAt, &checkout.Origin.ID, &checkout.Path); err != nil {
			return nil, err
		}
		bindings = append(bindings, configurationLocation{project: project, device: loc.device, workspace: loc.workspace, checkout: &checkout})
	}
	return bindings, rows.Err()
}
