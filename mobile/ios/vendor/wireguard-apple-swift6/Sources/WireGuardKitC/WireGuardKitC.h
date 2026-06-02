// SPDX-License-Identifier: MIT
// Copyright © 2018-2023 WireGuard LLC. All Rights Reserved.

// iogrid patch (#605 follow-up): Xcode 26's strict module import mode
// won't synthesize Darwin's unsigned typedefs (u_int32_t, u_char,
// u_int16_t) — they must be sourced explicitly from sys/types.h.
// Without this include the WireGuardKitC.pcm build fails with
// "declaration of 'u_int32_t' must be imported from module
// '_DarwinFoundation1.unsigned_types.u_int32_t' before it is required."
#include <sys/types.h>

#include "key.h"
#include "x25519.h"

/* From <sys/kern_control.h> */
#define CTLIOCGINFO 0xc0644e03UL
struct ctl_info {
    u_int32_t   ctl_id;
    char        ctl_name[96];
};
struct sockaddr_ctl {
    u_char      sc_len;
    u_char      sc_family;
    u_int16_t   ss_sysaddr;
    u_int32_t   sc_id;
    u_int32_t   sc_unit;
    u_int32_t   sc_reserved[5];
};
