export async function imageBlob(file: File, maxEdge = 2560): Promise<Blob> {
  if (!['image/jpeg', 'image/png', 'image/webp', 'image/avif'].includes(file.type) || file.size > 10 * 1024 * 1024)
    throw new Error('请选择 10 MB 以内的 JPG、PNG、WebP 或 AVIF 图片');
  const url = URL.createObjectURL(file);
  try {
    const image = new Image();
    image.src = url;
    await image.decode();
    if (!image.naturalWidth || image.naturalWidth * image.naturalHeight > 40000000) throw new Error('这张图片尺寸太大，请换一张');
    const ratio = Math.min(1, maxEdge / Math.max(image.naturalWidth, image.naturalHeight));
    const canvas = document.createElement('canvas');
    canvas.width = Math.max(1, Math.round(image.naturalWidth * ratio));
    canvas.height = Math.max(1, Math.round(image.naturalHeight * ratio));
    const context = canvas.getContext('2d');
    if (!context) throw new Error('暂时无法读取这张图片');
    context.fillStyle = '#ffffff';
    context.fillRect(0, 0, canvas.width, canvas.height);
    context.drawImage(image, 0, 0, canvas.width, canvas.height);
    return await new Promise<Blob>((resolve, reject) =>
      canvas.toBlob((b) => (b ? resolve(b) : reject(new Error('暂时无法读取这张图片'))), 'image/jpeg', 0.87),
    );
  } catch (error) {
    throw error instanceof Error ? error : new Error('这张图片无法打开，请换一张');
  } finally {
    URL.revokeObjectURL(url);
  }
}
