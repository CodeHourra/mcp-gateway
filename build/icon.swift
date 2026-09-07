import AppKit

// Native vector drawing keeps the app icon reproducible without image dependencies.
let destination = CommandLine.arguments[1]
for size in [16, 32, 128, 256, 512] {
    for scale in [1, 2] {
        let pixels = size * scale
        let bitmap = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: pixels, pixelsHigh: pixels, bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
        NSGraphicsContext.saveGraphicsState()
        NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: bitmap)
        NSGraphicsContext.current!.cgContext.clear(CGRect(x: 0, y: 0, width: pixels, height: pixels))
        let s = CGFloat(pixels) / 1024
        let transform = NSAffineTransform(); transform.scale(by: s); transform.concat()
        // Match macOS icon padding: keep the complete artwork at 85% around its centre.
        let inset = NSAffineTransform(); inset.translateX(by: 76.8, yBy: 76.8); inset.scale(by: 0.85); inset.concat()
        NSColor(calibratedRed: 0.06, green: 0.15, blue: 0.16, alpha: 1).setFill()
        NSBezierPath(roundedRect: NSRect(x: 28, y: 28, width: 968, height: 968), xRadius: 215, yRadius: 215).fill()
        let points = [NSPoint(x: 255, y: 725), NSPoint(x: 255, y: 300), NSPoint(x: 770, y: 512)]
        NSColor(calibratedRed: 0.47, green: 0.83, blue: 0.65, alpha: 1).setStroke()
        let path = NSBezierPath(); path.lineWidth = 43; path.lineCapStyle = .round
        for point in points { path.move(to: NSPoint(x: 500, y: 512)); path.line(to: point) }; path.stroke()
        for (index, point) in points.enumerated() {
            (index == 2 ? NSColor(calibratedRed: 0.91, green: 0.97, blue: 0.86, alpha: 1) : NSColor(calibratedRed: 0.47, green: 0.83, blue: 0.65, alpha: 1)).setFill()
            NSBezierPath(roundedRect: NSRect(x: point.x-87, y: point.y-87, width: 174, height: 174), xRadius: 45, yRadius: 45).fill()
        }
        NSColor.white.setFill(); NSBezierPath(ovalIn: NSRect(x: 438, y: 450, width: 124, height: 124)).fill()
        NSGraphicsContext.restoreGraphicsState()
        let suffix = scale == 2 ? "@2x" : ""
        try bitmap.representation(using: .png, properties: [:])!.write(to: URL(fileURLWithPath: "\(destination)/icon_\(size)x\(size)\(suffix).png"))
    }
}
