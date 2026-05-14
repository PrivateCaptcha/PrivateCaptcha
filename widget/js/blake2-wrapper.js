import * as b2wasm from 'blake2b-wasm';
import { blake2bInit, blake2bUpdate, blake2bFinal } from 'blakejs';

function Blake2b(outlen, key, salt, personal) {
    this._ctx = blake2bInit(outlen, key, salt, personal);
}

Blake2b.prototype.update = function(input) {
    blake2bUpdate(this._ctx, input);
    return this;
};

Blake2b.prototype.digest = function(out) {
    const result = blake2bFinal(this._ctx);
    if (out) {
        out.set(result);
        return out;
    }
    return result;
};

Blake2b.prototype.final = Blake2b.prototype.digest;

Blake2b.ready = function(cb) {
    b2wasm.ready(function() {
        cb() // ignore the error
    })
}

function createHash(outlen, key, salt, personal) {
    return new Blake2b(outlen, key, salt, personal);
}

export function ready(cb) {
    b2wasm.ready(function() {
        cb()
    })
}

export const WASM_SUPPORTED = b2wasm.SUPPORTED;
export let WASM_LOADED = false;
export let impl = createHash;

b2wasm.ready(function(err) {
    if (!err) {
        WASM_LOADED = true;
        impl = b2wasm.default;
    }
})
