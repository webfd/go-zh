// Copyright 2014 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

#define SIG_REGS(ctxt) (*((Sigcontext*)&((Ucontext*)(ctxt))->uc_mcontext)->regs)

#define SIG_R0(info, ctxt) (SIG_REGS(ctxt).gpr[0])
#define SIG_R1(info, ctxt) (SIG_REGS(ctxt).gpr[1])
#define SIG_R2(info, ctxt) (SIG_REGS(ctxt).gpr[2])
#define SIG_R3(info, ctxt) (SIG_REGS(ctxt).gpr[3])
#define SIG_R4(info, ctxt) (SIG_REGS(ctxt).gpr[4])
#define SIG_R5(info, ctxt) (SIG_REGS(ctxt).gpr[5])
#define SIG_R6(info, ctxt) (SIG_REGS(ctxt).gpr[6])
#define SIG_R7(info, ctxt) (SIG_REGS(ctxt).gpr[7])
#define SIG_R8(info, ctxt) (SIG_REGS(ctxt).gpr[8])
#define SIG_R9(info, ctxt) (SIG_REGS(ctxt).gpr[9])
#define SIG_R10(info, ctxt) (SIG_REGS(ctxt).gpr[10])
#define SIG_R11(info, ctxt) (SIG_REGS(ctxt).gpr[11])
#define SIG_R12(info, ctxt) (SIG_REGS(ctxt).gpr[12])
#define SIG_R13(info, ctxt) (SIG_REGS(ctxt).gpr[13])
#define SIG_R14(info, ctxt) (SIG_REGS(ctxt).gpr[14])
#define SIG_R15(info, ctxt) (SIG_REGS(ctxt).gpr[15])
#define SIG_R16(info, ctxt) (SIG_REGS(ctxt).gpr[16])
#define SIG_R17(info, ctxt) (SIG_REGS(ctxt).gpr[17])
#define SIG_R18(info, ctxt) (SIG_REGS(ctxt).gpr[18])
#define SIG_R19(info, ctxt) (SIG_REGS(ctxt).gpr[19])
#define SIG_R20(info, ctxt) (SIG_REGS(ctxt).gpr[20])
#define SIG_R21(info, ctxt) (SIG_REGS(ctxt).gpr[21])
#define SIG_R22(info, ctxt) (SIG_REGS(ctxt).gpr[22])
#define SIG_R23(info, ctxt) (SIG_REGS(ctxt).gpr[23])
#define SIG_R24(info, ctxt) (SIG_REGS(ctxt).gpr[24])
#define SIG_R25(info, ctxt) (SIG_REGS(ctxt).gpr[25])
#define SIG_R26(info, ctxt) (SIG_REGS(ctxt).gpr[26])
#define SIG_R27(info, ctxt) (SIG_REGS(ctxt).gpr[27])
#define SIG_R28(info, ctxt) (SIG_REGS(ctxt).gpr[28])
#define SIG_R29(info, ctxt) (SIG_REGS(ctxt).gpr[29])
#define SIG_R30(info, ctxt) (SIG_REGS(ctxt).gpr[30])
#define SIG_R31(info, ctxt) (SIG_REGS(ctxt).gpr[31])

#define SIG_SP(info, ctxt) (SIG_REGS(ctxt).gpr[1])
#define SIG_PC(info, ctxt) (SIG_REGS(ctxt).nip)
#define SIG_TRAP(info, ctxt) (SIG_REGS(ctxt).trap)
#define SIG_CTR(info, ctxt) (SIG_REGS(ctxt).ctr)
#define SIG_LINK(info, ctxt) (SIG_REGS(ctxt).link)
#define SIG_XER(info, ctxt) (SIG_REGS(ctxt).xer)
#define SIG_CCR(info, ctxt) (SIG_REGS(ctxt).ccr)

#define SIG_CODE0(info, ctxt) ((uintptr)(info)->si_code)
#define SIG_FAULT(info, ctxt) (SIG_REGS(ctxt).dar)
