// ADR-0243: source embedded and compiled once on the target, no build-host DXBC tool.
Texture2D yPlane : register(t0);
Texture2D uPlane : register(t1);
Texture2D vPlane : register(t2);
SamplerState linearSampler : register(s0);
cbuffer Color : register(b0) {
    float4 row0;
    float4 row1;
    float4 row2;
    float4 options; // NV12, crop scale x/y (allocation padding), unused
};
struct Vertex { float4 position : SV_POSITION; float2 uv : TEXCOORD0; };
Vertex vs_main(uint id : SV_VertexID) {
    Vertex o;
    o.uv = float2((id << 1) & 2, id & 2);
    o.position = float4(o.uv.x * 2 - 1, 1 - o.uv.y * 2, 0, 1);
    return o;
}
float4 ps_main(Vertex i) : SV_TARGET {
    float2 uv = i.uv * options.yz;
    float y = yPlane.Sample(linearSampler, uv).r;
    float2 chroma = options.x > 0.5 ? uPlane.Sample(linearSampler, uv).rg
                                  : float2(uPlane.Sample(linearSampler, uv).r, vPlane.Sample(linearSampler, uv).r);
    float4 sample = float4(y, chroma, 1);
    return float4(dot(row0, sample), dot(row1, sample), dot(row2, sample), 1);
}
