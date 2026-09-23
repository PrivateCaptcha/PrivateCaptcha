import { build, transform } from 'esbuild';
import { readFile } from 'node:fs/promises';
import inlineWorkerPlugin from 'esbuild-plugin-inline-worker';

const workerOptions = {
    entryPoints: ['./js/puzzle.worker.js'],
    bundle: true,
    minify: true,
    format: 'esm',
    loader: { '.wasm': 'base64' },
    write: false,
    metafile: true,
};

const cssPlugin = {
    name: 'CSSMinifyPlugin',
    setup(context) {
        context.onLoad({ filter: /\.css$/ }, async ({ path }) => {
            const css = await transform(await readFile(path), { loader: 'css', minify: true });
            return { loader: 'text', contents: css.code };
        });
    },
};

const [main, worker] = await Promise.all([
    build({
        entryPoints: ['./js/captcha.js'],
        bundle: true,
        minify: true,
        format: 'iife',
        loader: { '.css': 'text', '.wasm': 'base64' },
        plugins: [cssPlugin, inlineWorkerPlugin({ minify: true, loader: { '.wasm': 'base64' } })],
        write: false,
        metafile: true,
    }),
    build(workerOptions),
]);

for (const [name, result] of [['main', main], ['worker', worker]]) {
    const output = Object.values(result.metafile.outputs)[0];
    console.log(`${name}: ${result.outputFiles[0].contents.length} bytes`);
    const modules = Object.entries(output.inputs)
        .map(([path, details]) => ({ path, bytes: details.bytesInOutput }))
        .sort((left, right) => right.bytes - left.bytes);
    for (const module of modules.slice(0, 20)) {
        console.log(`  ${String(module.bytes).padStart(6)}  ${module.path}`);
    }
}
