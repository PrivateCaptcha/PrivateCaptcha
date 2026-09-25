import { build, transform } from 'esbuild';
import { readFile } from "fs/promises"
import { resolve, sep } from 'path';
import inlineWorkerPlugin from 'esbuild-plugin-inline-worker';

const testDirectory = resolve('test');

let RejectTestOnlyImportsPlugin = {
    name: 'RejectTestOnlyImportsPlugin',
    setup(build) {
        build.onResolve({ filter: /.*/ }, (args) => {
            if (!args.resolveDir) { return; }
            const resolved = resolve(args.resolveDir, args.path);
            if (resolved === testDirectory || resolved.startsWith(testDirectory + sep)) {
                return { errors: [{ text: `Production widget cannot import test-only code: ${args.path}` }] };
            }
        });
    },
};

let CSSMinifyPlugin = {
    name: "CSSMinifyPlugin",
    setup(build) {
        build.onLoad({ filter: /\.css$/ }, async (args) => {
            const f = await readFile(args.path)
            const css = await transform(f, { loader: "css", minify: false })
            return { loader: "text", contents: css.code }
        })
    }
}

function plugins(rejectTestImports = false) {
    const workerConfig = {
        minify: false,
        loader: { '.wasm': 'base64' },
    };
    if (rejectTestImports) {
        workerConfig.plugins = [RejectTestOnlyImportsPlugin];
    }
    const result = [
        CSSMinifyPlugin,
        inlineWorkerPlugin(workerConfig),
    ];
    if (rejectTestImports) {
        result.push(RejectTestOnlyImportsPlugin);
    }
    return result;
}

async function main() {
    const productionGraph = await build({
        entryPoints: ['./js/captcha.js', './js/widget.js'],
        bundle: true,
        outdir: './test/production-graph',
        write: false,
        metafile: true,
        loader: { '.css': 'text', '.wasm': 'base64' },
        plugins: plugins(true),
    });
    const testInputs = Object.keys(productionGraph.metafile.inputs)
        .filter((path) => path.startsWith('test/') || path.includes('/test/'));
    if (testInputs.length > 0) {
        throw new Error(`Test-only code leaked into the production widget graph: ${testInputs.join(', ')}`);
    }

    await build({
        entryPoints: ['./test/widget.test.js'],
        bundle: true,
        outfile: './test/bundle.test.js',
        format: 'esm',
        loader: { '.css': 'text', '.wasm': 'base64' },
        external: ['node:test', 'node:assert', 'node:fs/promises', 'node:http', 'happy-dom'],
        plugins: plugins(),
        minify: false,
        sourcemap: true,
    });

    await build({
        entryPoints: ['./test/widget.bench.js'],
        bundle: true,
        outfile: './test/bundle.bench.js',
        format: 'esm',
        platform: 'node',
        loader: { '.wasm': 'base64' },
        minify: false,
        sourcemap: true,
    });
}

main().catch((error) => {
    console.error(error);
    process.exit(1);
});
