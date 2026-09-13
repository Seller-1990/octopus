'use client';

import * as React from 'react';

import { cn } from '@/lib/utils';

function Table({ className, ...props }: React.ComponentProps<'table'>) {
    // 容器不设 overflow：中间滚动容器会让 thead/th 的 sticky 相对它计算，
    // 吸附失效；滚动（含横向）由使用方最外层容器承担
    return (
        <div data-slot="table-container" className="relative w-full">
            <table data-slot="table" className={cn('w-full caption-bottom text-sm', className)} {...props} />
        </div>
    );
}

function TableHeader({ className, ...props }: React.ComponentProps<'thead'>) {
    return <thead data-slot="table-header" className={cn('[&_tr]:border-b', className)} {...props} />;
}

function TableBody({ className, ...props }: React.ComponentProps<'tbody'>) {
    return <tbody data-slot="table-body" className={cn('[&_tr:last-child]:border-0', className)} {...props} />;
}

function TableFooter({ className, ...props }: React.ComponentProps<'tfoot'>) {
    return (
        <tfoot
            data-slot="table-footer"
            className={cn('bg-muted/50 border-t font-medium [&>tr]:last:border-b-0', className)}
            {...props}
        />
    );
}

function TableRow({ className, ...props }: React.ComponentProps<'tr'>) {
    return (
        <tr
            data-slot="table-row"
            className={cn('hover:bg-muted/40 data-[state=selected]:bg-muted/60 border-b transition-colors', className)}
            {...props}
        />
    );
}

function TableHead({ className, ...props }: React.ComponentProps<'th'>) {
    return (
        <th
            data-slot="table-head"
            className={cn(
                'h-11 whitespace-nowrap px-3 text-left align-middle text-[11px] font-medium uppercase tracking-wider text-muted-foreground [&:has([role=checkbox])]:pr-0',
                className
            )}
            {...props}
        />
    );
}

function TableCell({ className, ...props }: React.ComponentProps<'td'>) {
    return (
        <td
            data-slot="table-cell"
            className={cn('whitespace-nowrap px-3 py-3 align-middle [&:has([role=checkbox])]:pr-0', className)}
            {...props}
        />
    );
}

function TableCaption({ className, ...props }: React.ComponentProps<'caption'>) {
    return <caption data-slot="table-caption" className={cn('mt-4 text-sm text-muted-foreground', className)} {...props} />;
}

export { Table, TableHeader, TableBody, TableFooter, TableHead, TableRow, TableCell, TableCaption };
