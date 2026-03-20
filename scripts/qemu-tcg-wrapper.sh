#!/usr/bin/env bash
# qemu-tcg-wrapper: Makes Lima's QEMU invocation TCG (software-emulation) safe.
#
# Fixes applied:
#   1. -cpu host/kvm64/kvm32  →  -cpu max        (KVM-only CPU models)
#   2. -machine ...,accel=kvm →  remove accel=kvm  (strip KVM flag from machine)
#   3. -accel kvm/hvf         →  -accel tcg,thread=multi,tb-size=512
#   4. If only a read-only OVMF CODE pflash is present (no VARS pflash), a
#      writable VARS copy is created in the Lima instance directory and injected.
#
# Set as QEMU_SYSTEM_X86_64 via lima-up.sh to force TCG mode.

set -eo pipefail

args=()
i=0
argv=("$@")
len=${#argv[@]}

# Pass 1: collect pidfile path and CODE pflash path for later VARS injection.
code_pflash=""
has_vars_pflash=false
instance_dir=""

j=0
while [ "$j" -lt "$len" ]; do
    a="${argv[$j]}"
    case "$a" in
        -pidfile)
            j=$((j + 1))
            instance_dir="$(dirname "${argv[$j]}")"
            ;;
        -drive)
            j=$((j + 1))
            drv="${argv[$j]}"
            if [[ "$drv" == *"if=pflash"* ]]; then
                if [[ "$drv" == *"readonly=on"* ]]; then
                    code_pflash="$(echo "$drv" | sed -n 's/.*[,]file=\([^,]*\).*/\1/p; s/^file=\([^,]*\).*/\1/p' | head -1)"
                else
                    has_vars_pflash=true
                fi
            fi
            ;;
    esac
    j=$((j + 1))
done

# Pass 2: build the corrected arg list.
while [ "$i" -lt "$len" ]; do
    arg="${argv[$i]}"
    case "$arg" in
        -machine)
            i=$((i + 1))
            mach="${argv[$i]}"
            mach="${mach//,accel=kvm/}"
            mach="${mach//,accel=hvf/}"
            case "$mach" in
                *vmport=off*) : ;;
                q35*)         mach="${mach},vmport=off" ;;
            esac
            args+=("-machine" "$mach")
            ;;
        -accel)
            i=$((i + 1))
            accel_val="${argv[$i]}"
            case "$accel_val" in
                kvm | hvf) accel_val="tcg,thread=multi,tb-size=512" ;;
            esac
            args+=("-accel" "$accel_val")
            ;;
        -cpu)
            i=$((i + 1))
            cpu_val="${argv[$i]:-host}"
            case "$cpu_val" in
                host | kvm64 | kvm32) cpu_val="max" ;;
            esac
            args+=("-cpu" "$cpu_val")
            ;;
        -drive)
            i=$((i + 1))
            drv="${argv[$i]}"
            args+=("-drive" "$drv")
            # After CODE pflash, inject missing VARS pflash immediately.
            if [[ "$drv" == *"if=pflash"* && "$drv" == *"readonly=on"* ]] \
               && ! $has_vars_pflash \
               && [ -n "$code_pflash" ] \
               && [ -n "$instance_dir" ]; then
                vars_src="${code_pflash//_CODE_/_VARS_}"
                vars_src="${vars_src//edk2-x86_64-code/edk2-x86_64-vars}"
                vars_dst="${instance_dir}/ovmf_vars.fd"
                if [ -f "$vars_src" ] && [ ! -f "$vars_dst" ]; then
                    cp "$vars_src" "$vars_dst"
                fi
                if [ -f "$vars_dst" ]; then
                    args+=("-drive" "if=pflash,format=raw,file=${vars_dst}")
                    has_vars_pflash=true
                fi
            fi
            ;;
        *)
            args+=("$arg")
            ;;
    esac
    i=$((i + 1))
done

exec qemu-system-x86_64 "${args[@]}"

