import * as b2wasm from 'blake2b-wasm';
import { blake2bInit, blake2bUpdate, blake2bFinal } from 'blakejs';

function Blake2b(outlen, key, salt, personal) {
    return blake2bInit(outlen, key);
}

Blake2b.prototype.update = function(input) {
    // assert(input instanceof Uint8Array, 'input must be Uint8Array or Buffer')
    blake2bUpdate(this, input);
    return this;
}

Blake2b.prototype.digest = function(out) {
    var buf = (!out) ? new Uint8Array(this.outlen) : out
    // assert(buf instanceof Uint8Array, 'out must be "binary", "hex", Uint8Array, or Buffer')
    // assert(buf.length >= this.outlen, 'out must have at least outlen bytes of space')
    blake2bFinal(this, buf);
    return buf;
}

Blake2b.prototype.final = Blake2b.prototype.digest;

Blake2b.ready = function(cb) {
    b2wasm.ready(function() {
        cb() // ignore the error
    })
}

function createHash(outlen, key, salt, personal) {
    return new Blake2b(outlen, key);
}

export function ready(cb) {
    b2wasm.ready(function() {
        cb()
    })
}

export let WASM_SUPPORTED = b2wasm.SUPPORTED;
export let WASM_LOADED = false;
export let impl = createHash;

b2wasm.ready(function(err) {
    if (!err) {
        WASM_LOADED = true;
        impl = b2wasm.default;
    }
})
