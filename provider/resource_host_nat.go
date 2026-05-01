package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &HostNATResource{}
var _ resource.ResourceWithConfigure = &HostNATResource{}
var _ resource.ResourceWithImportState = &HostNATResource{}

type HostNATResource struct {
	client *Client
}

type hostNATModel struct {
	ID                types.String `tfsdk:"id"`
	NodeName          types.String `tfsdk:"node_name"`
	Bridge            types.String `tfsdk:"bridge"`
	SourceCIDR        types.String `tfsdk:"source_cidr"`
	OutboundInterface types.String `tfsdk:"outbound_interface"`
	BridgeCIDR        types.String `tfsdk:"bridge_cidr"`
	AllowedForward    types.List   `tfsdk:"allowed_forward_cidrs"`
	PortForwards      types.List   `tfsdk:"port_forwards"`
}

type portForwardModel struct {
	ListenPort types.Int64  `tfsdk:"listen_port"`
	TargetIP   types.String `tfsdk:"target_ip"`
	TargetPort types.Int64  `tfsdk:"target_port"`
	Protocol   types.String `tfsdk:"protocol"`
}

func NewHostNATResource() resource.Resource {
	return &HostNATResource{}
}

func (r *HostNATResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_host_nat"
}

func (r *HostNATResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Configura NAT no host Proxmox permitindo que uma rede local (ex.: vmbr1/10.11.0.0/24) saia pela interface principal.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Identificador interno node/cidr.",
			},
			"node_name": schema.StringAttribute{
				Required:    true,
				Description: "Nó alvo.",
			},
			"bridge": schema.StringAttribute{
				Required:    true,
				Description: "Bridge/NIC que representa a rede interna (ex.: vmbr1).",
			},
			"source_cidr": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Rede a ser mascarada (default 10.11.0.0/24).",
				Default:     stringdefault.StaticString("10.11.0.0/24"),
			},
			"bridge_cidr": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Endereço/prefixo aplicado à bridge (default 10.11.0.1/24).",
				Default:     stringdefault.StaticString("10.11.0.1/24"),
			},
			"outbound_interface": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Interface usada para saída (default vmbr0).",
				Default:     stringdefault.StaticString("vmbr0"),
			},
			"allowed_forward_cidrs": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Lista de sub-redes específicas com permissão para sair via NAT (default permite toda a source_cidr).",
			},
			"port_forwards": schema.ListNestedAttribute{
				Optional:    true,
				Description: "Lista de redirecionamentos (DNAT) para expor serviços internos via NAT.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"listen_port": schema.Int64Attribute{
							Required:    true,
							Description: "Porta no host para receber conexões externas.",
						},
						"target_ip": schema.StringAttribute{
							Required:    true,
							Description: "IP interno de destino.",
						},
						"target_port": schema.Int64Attribute{
							Optional:    true,
							Computed:    true,
							Description: "Porta de destino (default igual a listen_port).",
							Default:     int64default.StaticInt64(0),
						},
						"protocol": schema.StringAttribute{
							Optional:    true,
							Computed:    true,
							Description: "Protocolo (tcp/udp). Default tcp.",
							Default:     stringdefault.StaticString("tcp"),
						},
					},
				},
			},
		},
	}
}

func (r *HostNATResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		if defaultClient != nil {
			r.client = defaultClient
		}
		return
	}
	client, ok := req.ProviderData.(*Client)
	if !ok {
		resp.Diagnostics.AddError("Provider mal configurado", "Tipo inesperado em ProviderData.")
		return
	}
	r.client = client
}

func (r *HostNATResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan hostNATModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.applyNAT(ctx, plan); err != nil {
		resp.Diagnostics.AddError("Falha ao configurar NAT", err.Error())
		return
	}
	plan.ID = types.StringValue(fmt.Sprintf("%s:%s", plan.NodeName.ValueString(), plan.SourceCIDR.ValueString()))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *HostNATResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state hostNATModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	client := r.client
	if client == nil && defaultClient != nil {
		client = defaultClient
	}
	if client != nil && client.SSHUser != "" && (client.SSHKey != "" || client.SSHPass != "") {
		bridge := state.Bridge.ValueString()
		bridgeCIDR := state.BridgeCIDR.ValueString()
		if bridge != "" && bridgeCIDR != "" {
			if ok, err := r.bridgeHasCIDR(ctx, client, state, bridge, bridgeCIDR); err == nil && !ok {
				state.BridgeCIDR = types.StringNull()
			}
		}
	}
	state.ID = types.StringValue(fmt.Sprintf("%s:%s", state.NodeName.ValueString(), state.SourceCIDR.ValueString()))
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *HostNATResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan hostNATModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.applyNAT(ctx, plan); err != nil {
		resp.Diagnostics.AddError("Falha ao atualizar NAT", err.Error())
		return
	}
	plan.ID = types.StringValue(fmt.Sprintf("%s:%s", plan.NodeName.ValueString(), plan.SourceCIDR.ValueString()))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *HostNATResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state hostNATModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	client := r.client
	if client == nil && defaultClient != nil {
		client = defaultClient
	}
	if client == nil {
		return
	}
	host := state.NodeName.ValueString()
	if client.SSHHost != "" {
		host = client.SSHHost
	}
	var allowed []string
	if !state.AllowedForward.IsNull() && !state.AllowedForward.IsUnknown() {
		state.AllowedForward.ElementsAs(ctx, &allowed, false)
	}
	forwards, diags := expandPortForwards(ctx, state.PortForwards)
	if diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}
	cleanupRules := buildForwardCleanup(state.SourceCIDR.ValueString(), state.OutboundInterface.ValueString(), allowed, forwards)
	_ = r.execHost(ctx, client, host, cleanupRules)
	cleanup := "systemctl disable --now pxgrid-nat.service >/dev/null 2>&1 || true; rm -f /etc/systemd/system/pxgrid-nat.service /usr/local/bin/pxgrid-nat.sh /etc/sysctl.d/99-pxgrid-forward.conf; systemctl daemon-reload || true"
	_ = r.execHost(ctx, client, host, cleanup)
}

func (r *HostNATResource) applyNAT(ctx context.Context, plan hostNATModel) error {
	client := r.client
	if client == nil && defaultClient != nil {
		client = defaultClient
	}
	if client == nil {
		return fmt.Errorf("provider não configurado")
	}
	if client.SSHUser == "" {
		return fmt.Errorf("ssh ausente: defina ssh_username no provider para usar pxgrid_host_nat")
	}
	if client.SSHKey == "" && client.SSHPass == "" {
		return fmt.Errorf("ssh ausente: informe ssh_private_key(_file) ou ssh_password para pxgrid_host_nat")
	}
	host := plan.NodeName.ValueString()
	if client.SSHHost != "" {
		host = client.SSHHost
	}
	bridge := plan.Bridge.ValueString()
	if bridge == "" {
		return fmt.Errorf("bridge obrigatoria")
	}
	sourceCIDR := plan.SourceCIDR.ValueString()
	outIface := plan.OutboundInterface.ValueString()
	bridgeCIDR := plan.BridgeCIDR.ValueString()
	var allowed []string
	if !plan.AllowedForward.IsNull() && !plan.AllowedForward.IsUnknown() {
		plan.AllowedForward.ElementsAs(ctx, &allowed, false)
	}
	forwards, diags := expandPortForwards(ctx, plan.PortForwards)
	if diags.HasError() {
		return fmt.Errorf("port_forwards invalidos: %s", diagErrors(diags))
	}

	setupBridge := fmt.Sprintf(`set -euo pipefail
if ! ip link show %[1]s >/dev/null 2>&1; then
  ip link add name %[1]s type bridge
fi
ip addr replace %[2]s dev %[1]s
ip link set %[1]s up
`, bridge, bridgeCIDR)
	if err := r.execHost(ctx, client, host, setupBridge); err != nil {
		return fmt.Errorf("falha ao configurar bridge: %w", err)
	}
	sysctlCmd := "cat <<'EOF' >/etc/sysctl.d/99-pxgrid-forward.conf\nnet.ipv4.ip_forward = 1\nEOF\nsysctl -p /etc/sysctl.d/99-pxgrid-forward.conf >/dev/null 2>&1 || sysctl -w net.ipv4.ip_forward=1 >/dev/null 2>&1"
	if err := r.execHost(ctx, client, host, sysctlCmd); err != nil {
		return fmt.Errorf("falha ao configurar ip_forward: %w", err)
	}

	forwardRules := buildForwardRules(sourceCIDR, outIface, allowed)
	portForwardRules := buildPortForwardRules(outIface, forwards)

	script := fmt.Sprintf(`cat <<'EOF' >/usr/local/bin/pxgrid-nat.sh
#!/bin/bash
set -euo pipefail
IPTABLES_BIN=$(command -v iptables || echo /usr/sbin/iptables)
$IPTABLES_BIN -t nat -C POSTROUTING -s %[1]s -o %[2]s -j MASQUERADE 2>/dev/null || $IPTABLES_BIN -t nat -A POSTROUTING -s %[1]s -o %[2]s -j MASQUERADE
%[3]s
%[4]s
$IPTABLES_BIN -C FORWARD -d %[1]s -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT 2>/dev/null || $IPTABLES_BIN -A FORWARD -d %[1]s -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT
EOF
chmod +x /usr/local/bin/pxgrid-nat.sh
/usr/local/bin/pxgrid-nat.sh
`, sourceCIDR, outIface, forwardRules, portForwardRules)
	if err := r.execHost(ctx, client, host, script); err != nil {
		return fmt.Errorf("falha ao configurar NAT: %w", err)
	}
	service := `cat <<'EOF' >/etc/systemd/system/pxgrid-nat.service
[Unit]
Description=pxgrid NAT rules
After=network-online.target

[Service]
Type=oneshot
ExecStart=/usr/local/bin/pxgrid-nat.sh
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now pxgrid-nat.service
`
	if err := r.execHost(ctx, client, host, service); err != nil {
		return fmt.Errorf("falha ao habilitar servico NAT: %w", err)
	}
	if len(allowed) > 0 {
		vals := make([]attr.Value, len(allowed))
		for i, v := range allowed {
			vals[i] = types.StringValue(v)
		}
		plan.AllowedForward = types.ListValueMust(types.StringType, vals)
	} else {
		plan.AllowedForward = types.ListNull(types.StringType)
	}
	if len(forwards) == 0 {
		plan.PortForwards = types.ListNull(types.ObjectType{AttrTypes: map[string]attr.Type{
			"listen_port": types.Int64Type,
			"target_ip":   types.StringType,
			"target_port": types.Int64Type,
			"protocol":    types.StringType,
		}})
	}
	return nil
}

func (r *HostNATResource) bridgeHasCIDR(ctx context.Context, client *Client, plan hostNATModel, bridge, cidr string) (bool, error) {
	host := plan.NodeName.ValueString()
	if client.SSHHost != "" {
		host = client.SSHHost
	}
	output, err := client.RunSSHCommandOutput(ctx, host, []string{"bash", "-lc", fmt.Sprintf("ip -4 -o addr show dev %s | awk '{print $4}'", shellEscape(bridge))})
	if err != nil {
		return false, err
	}
	for _, line := range strings.Fields(output) {
		if line == cidr {
			return true, nil
		}
	}
	return false, nil
}

func (r *HostNATResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

func (r *HostNATResource) execHost(ctx context.Context, client *Client, host, command string) error {
	if client.SSHPass != "" {
		return client.RunSSHCommandPassword(ctx, host, client.SSHUser, client.SSHPass, command)
	}
	return client.RunSSHCommand(ctx, host, []string{"bash", "-lc", command})
}

func buildForwardRules(source, outbound string, allowed []string) string {
	var b strings.Builder
	if len(allowed) == 0 {
		fmt.Fprintf(&b, "$IPTABLES_BIN -C FORWARD -s %s -o %s -j ACCEPT 2>/dev/null || $IPTABLES_BIN -A FORWARD -s %s -o %s -j ACCEPT\n", source, outbound, source, outbound)
	} else {
		for _, cidr := range allowed {
			fmt.Fprintf(&b, "$IPTABLES_BIN -C FORWARD -s %s -o %s -j ACCEPT 2>/dev/null || $IPTABLES_BIN -A FORWARD -s %s -o %s -j ACCEPT\n", cidr, outbound, cidr, outbound)
		}
		fmt.Fprintf(&b, "$IPTABLES_BIN -C FORWARD -s %s -o %s -j DROP 2>/dev/null || $IPTABLES_BIN -A FORWARD -s %s -o %s -j DROP\n", source, outbound, source, outbound)
	}
	return b.String()
}

func buildPortForwardRules(outbound string, forwards []portForward) string {
	var b strings.Builder
	for _, pf := range forwards {
		proto := strings.ToLower(pf.Protocol)
		if proto == "" {
			proto = "tcp"
		}
		fmt.Fprintf(&b, "$IPTABLES_BIN -t nat -C PREROUTING -i %s -p %s --dport %d -j DNAT --to-destination %s:%d 2>/dev/null || $IPTABLES_BIN -t nat -A PREROUTING -i %s -p %s --dport %d -j DNAT --to-destination %s:%d\n", outbound, proto, pf.ListenPort, pf.TargetIP, pf.TargetPort, outbound, proto, pf.ListenPort, pf.TargetIP, pf.TargetPort)
		fmt.Fprintf(&b, "$IPTABLES_BIN -C FORWARD -p %s -d %s --dport %d -j ACCEPT 2>/dev/null || $IPTABLES_BIN -A FORWARD -p %s -d %s --dport %d -j ACCEPT\n", proto, pf.TargetIP, pf.TargetPort, proto, pf.TargetIP, pf.TargetPort)
	}
	return b.String()
}

type portForward struct {
	ListenPort int64
	TargetIP   string
	TargetPort int64
	Protocol   string
}

func expandPortForwards(ctx context.Context, list types.List) ([]portForward, diag.Diagnostics) {
	var diags diag.Diagnostics
	if list.IsNull() || list.IsUnknown() {
		return nil, diags
	}
	var raw []portForwardModel
	if d := list.ElementsAs(ctx, &raw, false); d.HasError() {
		return nil, d
	}
	out := make([]portForward, 0, len(raw))
	for _, pf := range raw {
		listen := pf.ListenPort.ValueInt64()
		target := pf.TargetIP.ValueString()
		targetPort := pf.TargetPort.ValueInt64()
		if listen <= 0 || listen > 65535 {
			diags.AddError("listen_port invalida", "listen_port deve estar entre 1 e 65535.")
			continue
		}
		if target == "" {
			diags.AddError("target_ip invalido", "target_ip e obrigatorio.")
			continue
		}
		if targetPort == 0 {
			targetPort = listen
		}
		proto := strings.ToLower(pf.Protocol.ValueString())
		if proto == "" {
			proto = "tcp"
		}
		if proto != "tcp" && proto != "udp" {
			diags.AddError("protocol invalido", "protocol deve ser tcp ou udp.")
			continue
		}
		out = append(out, portForward{
			ListenPort: listen,
			TargetIP:   target,
			TargetPort: targetPort,
			Protocol:   proto,
		})
	}
	return out, diags
}

func diagErrors(diags diag.Diagnostics) string {
	var parts []string
	for _, d := range diags {
		if d.Severity() == diag.SeverityError {
			parts = append(parts, d.Summary())
		}
	}
	return strings.Join(parts, "; ")
}

func buildForwardCleanup(source, outbound string, allowed []string, forwards []portForward) string {
	var b strings.Builder
	b.WriteString(`set -euo pipefail
IPTABLES_BIN=$(command -v iptables || echo /usr/sbin/iptables)
`)
	if len(allowed) == 0 {
		fmt.Fprintf(&b, "$IPTABLES_BIN -D FORWARD -s %s -o %s -j ACCEPT >/dev/null 2>&1 || true\n", source, outbound)
	} else {
		for _, cidr := range allowed {
			fmt.Fprintf(&b, "$IPTABLES_BIN -D FORWARD -s %s -o %s -j ACCEPT >/dev/null 2>&1 || true\n", cidr, outbound)
		}
		fmt.Fprintf(&b, "$IPTABLES_BIN -D FORWARD -s %s -o %s -j DROP >/dev/null 2>&1 || true\n", source, outbound)
	}
	for _, pf := range forwards {
		proto := strings.ToLower(pf.Protocol)
		if proto == "" {
			proto = "tcp"
		}
		fmt.Fprintf(&b, "$IPTABLES_BIN -t nat -D PREROUTING -i %s -p %s --dport %d -j DNAT --to-destination %s:%d >/dev/null 2>&1 || true\n", outbound, proto, pf.ListenPort, pf.TargetIP, pf.TargetPort)
		fmt.Fprintf(&b, "$IPTABLES_BIN -D FORWARD -p %s -d %s --dport %d -j ACCEPT >/dev/null 2>&1 || true\n", proto, pf.TargetIP, pf.TargetPort)
	}
	fmt.Fprintf(&b, "$IPTABLES_BIN -D FORWARD -d %s -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT >/dev/null 2>&1 || true\n", source)
	fmt.Fprintf(&b, "$IPTABLES_BIN -t nat -D POSTROUTING -s %s -o %s -j MASQUERADE >/dev/null 2>&1 || true\n", source, outbound)
	return b.String()
}
