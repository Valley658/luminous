let imagePreviewWasmPromise = null;

function loadImagePreviewWasm(wasmUrl) {
    if (!imagePreviewWasmPromise) {
        imagePreviewWasmPromise = fetch(wasmUrl)
            .then(resp => resp.arrayBuffer())
            .then(bytes => WebAssembly.instantiate(bytes, {}))
            .then(result => result.instance.exports);
    }
    return imagePreviewWasmPromise;
}

function computePreviewTargetSize(srcW, srcH, maxDimension) {
    const longest = Math.max(srcW, srcH);
    if (longest <= maxDimension) return [srcW, srcH];
    const scale = maxDimension / longest;
    return [Math.max(1, Math.round(srcW * scale)), Math.max(1, Math.round(srcH * scale))];
}

async function renderResizedPreview(file, wasmUrl, maxDimension) {
    if (file.type === "image/gif" || /\.gif$/i.test(file.name || "")) {
        const objectUrl = URL.createObjectURL(file);
        const dims = await new Promise(resolve => {
            const probe = new Image();
            probe.onload = () => resolve({ w: probe.naturalWidth, h: probe.naturalHeight });
            probe.onerror = () => resolve({ w: 0, h: 0 });
            probe.src = objectUrl;
        });
        return {
            dataUrl: objectUrl,
            width: dims.w,
            height: dims.h,
            originalWidth: dims.w,
            originalHeight: dims.h,
            isAnimated: true,
        };
    }
    const bitmap = await createImageBitmap(file, { imageOrientation: "from-image" });
    const srcCanvas = document.createElement("canvas");
    srcCanvas.width = bitmap.width;
    srcCanvas.height = bitmap.height;
    const srcCtx = srcCanvas.getContext("2d");
    srcCtx.drawImage(bitmap, 0, 0);
    const srcData = srcCtx.getImageData(0, 0, bitmap.width, bitmap.height);

    const [dstW, dstH] = computePreviewTargetSize(bitmap.width, bitmap.height, maxDimension);

    const wasm = await loadImagePreviewWasm(wasmUrl);
    wasm.reset();
    const srcSize = bitmap.width * bitmap.height * 4;
    const srcPtr = wasm.alloc(srcSize);
    new Uint8Array(wasm.memory.buffer, srcPtr, srcSize).set(srcData.data);

    const dstPtr = wasm.resizeBilinear(srcPtr, bitmap.width, bitmap.height, dstW, dstH);
    const dstBytes = new Uint8ClampedArray(wasm.memory.buffer.slice(dstPtr, dstPtr + dstW * dstH * 4));

    const dstCanvas = document.createElement("canvas");
    dstCanvas.width = dstW;
    dstCanvas.height = dstH;
    dstCanvas.getContext("2d").putImageData(new ImageData(dstBytes, dstW, dstH), 0, 0);

    return {
        dataUrl: dstCanvas.toDataURL("image/webp", 0.9),
        width: dstW,
        height: dstH,
        originalWidth: bitmap.width,
        originalHeight: bitmap.height,
    };
}
