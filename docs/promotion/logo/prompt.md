# MCP Gateway Logo v1

生成方式：内置 imagegen。交付：[mcp-gateway-logo-v1.png](mcp-gateway-logo-v1.png)，1254 × 1254 PNG，墨绿背景。

设计含义：左侧两个节点通过中心汇聚到右侧统一入口；延续推广图的墨绿、薄荷绿和浅绿配色。作为独立品牌素材保存。

## 初始生成提示词

```text
Use case: logo-brand.
Create ONE polished standalone brand symbol for "MCP Gateway", a macOS utility that connects multiple MCP services through one local gateway. This is a finished raster logo asset, not a presentation board or mockup.
Canvas: square 1024x1024 PNG. Genuinely transparent background outside the icon, preserving alpha; no fake checkerboard.
Visual identity: deep forest green #102D2D, soft mint #78D4A6, pale mint #E8F7DC and a white central hub. Calm, precise, contemporary developer-tool identity, aligned with the existing MCP Gateway promotion campaign.
Composition: a single large forest-green rounded-square tile, perfectly centered, occupying about 84% of canvas width with equal transparent margin on all sides. Inside the tile, draw one compact geometric gateway mark. Two mint rounded-square nodes on the upper-left and lower-left connect by thick clean rounded mint strokes into a small white circular central hub; from that hub a horizontal mint stroke connects to one pale-mint rounded-square node on the right. Exactly THREE endpoint nodes and ONE central hub. Endpoint nodes equal size; central hub slightly smaller. The structure reads naturally as multiple connections converging into one gateway, with balanced negative space. Nodes and connectors remain unmistakable at small avatar/icon sizes. Use generous spacing between nodes and the outer tile edge.
Style: flat, clean, carefully designed geometric logo with smooth corners, optical balance and crisp edges. Solid colors, consistent connector thickness. No gradients, bevels, glossy highlights, shadows, paper grain, texture, 3D, glass, surrounding frames, extra flourishes, or photography.
No lettering, wordmark, slogan, numbers, labels, watermark, diagram arrows or extra objects. Do not show multiple alternatives. Output only the single centered logo icon on transparent canvas.
```

## 最终修订提示词

首版透明底出现不规则透明区域，最终改为完整的不透明墨绿背景。

```text
Use case: precise-object-edit.
Input image: edit target, the attached MCP Gateway logo.
Create a clean final logo asset by fixing the background of this exact design. Preserve the white central hub, the two mint rounded-square left nodes, the pale-mint right node, and their connecting strokes, their relative positions and overall silhouette. Preserve the same forest green and mint brand palette. Smooth and simplify all color areas to flat solid fills with crisp antialiased outlines.
CRITICAL CHANGE: Replace the entire canvas background with a COMPLETELY OPAQUE, uniform dark forest green (#102D2D), extending edge to edge on all four sides. No transparent pixels, NO transparency, NO alpha cutouts, NO black patches, NO holes, NO rounded outer tile boundary, no outer margin of another color. The entire background is one unbroken solid green square. All the mark's nodes and connector strokes are also fully opaque and evenly colored. Remove ALL grain, mottling, lighting, gradients, shadows and material texture.
Composition: square canvas. Keep the three-node gateway mark optically centered, occupying approximately 66% of the canvas, with generous equal negative space. Endpoint nodes are the same size and central white circle is smaller. The final product is a simple flat logo on a perfectly solid dark green square background, useful as an avatar.
No text, no wordmark, no extra icons, no watermark, no mockup. One final logo only.
```
